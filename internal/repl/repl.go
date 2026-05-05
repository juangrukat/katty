package repl

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/kat/katty/internal/builtin"
	"github.com/kat/katty/internal/config"
	"github.com/kat/katty/internal/deepseek"
	"github.com/kat/katty/internal/mcp"
	"github.com/kat/katty/internal/systemctx"
	"github.com/kat/katty/internal/transcript"
)

type REPL struct {
	cfg        *config.Config
	ds         *deepseek.Client
	builtins   *builtin.Registry
	mcpMgr     *mcp.Manager
	sysCtx     *systemctx.SystemContext
	transcript *transcript.Logger
	messages   []deepseek.Message
	reader     *bufio.Reader
	running    bool
	mu         sync.Mutex

	// State for Ctrl-C
	turnCancel context.CancelFunc
	lastCtrlC  time.Time
}

func New(cfg *config.Config, ds *deepseek.Client, builtins *builtin.Registry, mcpMgr *mcp.Manager, sysCtx *systemctx.SystemContext, ts *transcript.Logger) *REPL {
	return &REPL{
		cfg:        cfg,
		ds:         ds,
		builtins:   builtins,
		mcpMgr:     mcpMgr,
		sysCtx:     sysCtx,
		transcript: ts,
		reader:     bufio.NewReader(os.Stdin),
		running:    true,
	}
}

func (r *REPL) Run() {
	// Set up signal handling
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT)

	go func() {
		for range sigCh {
			r.handleCtrlC()
		}
	}()

	// Set system message
	sysPrompt := r.sysCtx.Prompt()
	r.messages = append(r.messages, deepseek.Message{Role: "system", Content: sysPrompt})

	fmt.Println("Katty ready. Type /help for commands, /exit to quit.")
	fmt.Println()

	for r.running {
		fmt.Print("> ")
		input, err := r.reader.ReadString('\n')
		if err != nil {
			if r.running {
				fmt.Fprintf(os.Stderr, "read error: %v\n", err)
			}
			break
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Handle REPL commands
		if strings.HasPrefix(input, "/") {
			r.handleCommand(input)
			continue
		}

		// User message
		r.transcript.Log(transcript.TypeUser, input, nil)
		r.messages = append(r.messages, deepseek.Message{Role: "user", Content: input})

		// Run tool call loop
		r.runToolLoop()
	}
}

func (r *REPL) handleCtrlC() {
	now := time.Now()
	if now.Sub(r.lastCtrlC) < 2*time.Second {
		fmt.Println("\nDouble Ctrl-C: exiting.")
		r.running = false
		os.Exit(0)
	}
	r.lastCtrlC = now

	r.mu.Lock()
	if r.turnCancel != nil {
		r.turnCancel()
		r.turnCancel = nil
	}
	r.mu.Unlock()

	fmt.Println("\nInterrupted. Returning to prompt.")
}

func (r *REPL) runToolLoop() {
	maxRounds := r.cfg.Tooling.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = 5
	}

	for round := 0; round < maxRounds; round++ {
		// Create per-turn context
		turnCtx, cancel := context.WithCancel(context.Background())
		r.mu.Lock()
		r.turnCancel = cancel
		r.mu.Unlock()

		resp, err := r.ds.Chat(turnCtx, r.messages)

		r.mu.Lock()
		r.turnCancel = nil
		r.mu.Unlock()

		if err != nil {
			fmt.Fprintf(os.Stderr, "DeepSeek error: %v\n", err)
			return
		}

		if len(resp.Choices) == 0 {
			fmt.Println("(no response)")
			return
		}

		assistantMsg := resp.Choices[0].Message.Content
		r.transcript.Log(transcript.TypeAssistant, assistantMsg, nil)
		r.messages = append(r.messages, deepseek.Message{Role: "assistant", Content: assistantMsg})

		// Parse katty_tool_call blocks
		toolCalls := parseToolCalls(assistantMsg)

		if len(toolCalls) == 0 {
			// Check for dangling action
			if isDanglingAction(assistantMsg) {
				controlMsg := `<katty_control>
You indicated you were about to use a local tool, but you did not emit a <katty_tool_call> block.
Either emit the exact <katty_tool_call> now, or give a final answer without claiming you will check anything.
</katty_control>`
				r.messages = append(r.messages, deepseek.Message{Role: "user", Content: controlMsg})
				continue
			}

			fmt.Println(assistantMsg)
			return
		}

		// Execute tools
		for _, tc := range toolCalls {
			result := r.executeTool(turnCtx, tc)
			r.messages = append(r.messages, deepseek.Message{Role: "user", Content: result})
		}
	}

	fmt.Println("Tool loop stopped after reaching max_tool_rounds=5.")
}

type toolCall struct {
	Server string                 `json:"server"`
	Tool   string                 `json:"tool"`
	Args   map[string]interface{} `json:"args"`
}

func parseToolCalls(text string) []toolCall {
	var calls []toolCall

	for {
		start := strings.Index(text, "<katty_tool_call>")
		if start == -1 {
			break
		}
		start += len("<katty_tool_call>")

		end := strings.Index(text[start:], "</katty_tool_call>")
		if end == -1 {
			break
		}

		jsonStr := strings.TrimSpace(text[start : start+end])
		text = text[start+end+len("</katty_tool_call>"):]

		var tc toolCall
		if err := json.Unmarshal([]byte(jsonStr), &tc); err != nil {
			continue
		}
		calls = append(calls, tc)
	}

	return calls
}

func isDanglingAction(text string) bool {
	lower := strings.ToLower(text)
	danglingPhrases := []string{
		"let me check", "let me run", "let me try",
		"let me inspect", "let me verify",
		"i'll check", "i'll run", "i'll try",
		"checking", "i will check", "i will run",
	}

	for _, phrase := range danglingPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}

	// Check if ends with colon after action announcement
	trimmed := strings.TrimSpace(text)
	if strings.HasSuffix(trimmed, ":") {
		actionWords := []string{"let me", "i'll", "i will", "running", "checking", "trying"}
		for _, w := range actionWords {
			if strings.Contains(strings.ToLower(trimmed), w) {
				return true
			}
		}
	}

	return false
}

func (r *REPL) executeTool(ctx context.Context, tc toolCall) string {
	start := time.Now()

	// Build fully qualified name: server.tool, but avoid double-prefixing
	// if the tool name is already fully qualified (e.g. "katty.fs.list")
	fullName := tc.Tool
	if !strings.HasPrefix(tc.Tool, tc.Server+".") {
		fullName = tc.Server + "." + tc.Tool
	}

	// Check if it's a built-in (server is "katty")
	if bt := r.builtins.Get(fullName); bt != nil {
		result := bt.Handler(ctx, tc.Args)
		elapsed := time.Since(start)

		if result.IsError {
			r.transcript.Log(transcript.TypeToolError, tc.Tool, map[string]interface{}{
				"error": result.Error,
				"args":  tc.Args,
			})
			return fmt.Sprintf(`<tool_result server="katty" tool="%s" elapsed_ms="%d">
tool_error: %s
message: %s
</tool_result>`, tc.Tool, elapsed.Milliseconds(), result.Error.Kind, result.Error.Message)
		}

		r.transcript.Log(transcript.TypeToolResult, tc.Tool, map[string]interface{}{
			"content": result.Content,
			"args":    tc.Args,
		})

		contentJSON, _ := json.Marshal(result.Content)
		return fmt.Sprintf(`<tool_result server="katty" tool="%s" elapsed_ms="%d">
%s
</tool_result>`, tc.Tool, elapsed.Milliseconds(), string(contentJSON))
	}

	// Check MCP
	if r.mcpMgr != nil && tc.Server != "katty" {
		mcpResult, err := r.mcpMgr.CallTool(ctx, tc.Server, tc.Tool, tc.Args)
		elapsed := time.Since(start)

		if err != nil {
			return fmt.Sprintf(`<tool_result server="%s" tool="%s" elapsed_ms="%d">
tool_error: %s
</tool_result>`, tc.Server, tc.Tool, elapsed.Milliseconds(), err.Error())
		}

		normalized := mcp.NormalizeResult(mcpResult)
		contentJSON, _ := json.Marshal(normalized)
		return fmt.Sprintf(`<tool_result server="%s" tool="%s" elapsed_ms="%d">
%s
</tool_result>`, tc.Server, tc.Tool, elapsed.Milliseconds(), string(contentJSON))
	}

	return fmt.Sprintf(`<tool_result server="%s" tool="%s">
tool_error: not_found
message: tool not found
</tool_result>`, tc.Server, tc.Tool)
}

func (r *REPL) handleCommand(input string) {
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return
	}
	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "/help":
		fmt.Println(`Katty REPL commands:
  /help           - This help
  /env            - Print environment block
  /capabilities   - Print detected capability families
  /files          - List loaded startup files
  /targets        - List configured targets
  /mcp            - Show MCP server states
  /tools          - List built-in and MCP tools
  /schema <tool>  - Print tool schema
  /tool <tool> <json_args> - Call a tool directly
  /call <tool> <json_args> - Alias for /tool
  /interrupt      - Cancel current operation
  /ps             - Show active child processes
  /sessions       - List active sessions
  /kill           - Kill active non-MCP exec subprocesses
  /reset-terminal - Reset terminal state
  /reload         - Reload config
  /system         - Print system context
  /doctor         - Run diagnostics
  /debug messages - Show recent message roles and char counts
  /debug last     - Show last raw tool response
  /exit           - Exit Katty`)

	case "/env":
		fmt.Println("Environment:")
		fmt.Printf("  OS: %s\n", r.sysCtx.Env.OS)
		fmt.Printf("  Arch: %s\n", r.sysCtx.Env.Arch)
		fmt.Printf("  Uname: %s\n", r.sysCtx.Env.Uname)
		fmt.Printf("  User: %s\n", r.sysCtx.Env.Username)
		fmt.Printf("  Home: %s\n", r.sysCtx.Env.Home)
		fmt.Printf("  Shell: %s\n", r.sysCtx.Env.Shell)
		fmt.Printf("  CWD: %s\n", r.sysCtx.Env.CWD)
		fmt.Printf("  Tools found: %d\n", len(r.sysCtx.Env.ToolPaths))

	case "/capabilities":
		fmt.Println("Capability families:")
		for family, tools := range r.sysCtx.Capabilities {
			if len(tools) > 0 {
				fmt.Printf("  %s: %s\n", family, strings.Join(tools, ", "))
			}
		}

	case "/files":
		fmt.Println("Startup files:")
		for _, f := range r.sysCtx.StartupFiles {
			fmt.Printf("  %s (%d chars)\n", f.Path, f.Size)
		}

	case "/targets":
		fmt.Println("Targets:")
		for name, t := range r.sysCtx.Targets {
			fmt.Printf("  %s: type=%s", name, t.Type)
			if t.Default {
				fmt.Print(" (default)")
			}
			fmt.Println()
		}

	case "/mcp":
		if r.mcpMgr == nil {
			fmt.Println("No MCP manager.")
			return
		}
		for _, s := range r.mcpMgr.ListServers() {
			fmt.Printf("  %s: %s", s.Name, s.State)
			if s.InitError != "" {
				fmt.Printf(" (%s)", s.InitError)
			}
			fmt.Printf(" (%d tools)\n", len(s.Tools))
		}

	case "/tools":
		fmt.Println("Built-in tools:")
		for _, t := range r.builtins.List() {
			fmt.Printf("  %s - %s\n", t.Name, t.Description)
		}
		if r.mcpMgr != nil {
			fmt.Println("\nMCP tools:")
			for _, t := range r.mcpMgr.ListTools() {
				fmt.Printf("  %s - %s\n", t.Name, t.Description)
			}
		}

	case "/schema":
		if len(args) < 1 {
			fmt.Println("Usage: /schema <tool_name>")
			return
		}
		toolName := args[0]
		if bt := r.builtins.Get(toolName); bt != nil {
			schema, _ := json.MarshalIndent(bt.Schema, "", "  ")
			fmt.Println(string(schema))
		} else if r.mcpMgr != nil {
			schema, err := r.mcpMgr.GetSchema(toolName)
			if err != nil {
				fmt.Printf("Schema not found: %s\n", toolName)
				return
			}
			s, _ := json.MarshalIndent(schema, "", "  ")
			fmt.Println(string(s))
		} else {
			fmt.Printf("Tool not found: %s\n", toolName)
		}

	case "/tool", "/call":
		if len(args) < 2 {
			fmt.Println("Usage: /tool <tool_name> <json_args>")
			return
		}
		toolName := args[0]
		jsonArgs := strings.Join(args[1:], " ")
		var toolArgs map[string]interface{}
		if err := json.Unmarshal([]byte(jsonArgs), &toolArgs); err != nil {
			fmt.Printf("Invalid JSON args: %v\n", err)
			return
		}

		ctx := context.Background()
		result := r.executeTool(ctx, toolCall{Server: "katty", Tool: toolName, Args: toolArgs})
		fmt.Println(result)

	case "/interrupt":
		r.handleCtrlC()

	case "/ps":
		ctx := context.Background()
		result := builtin.ProcPsDirect(ctx)
		fmt.Println(result)

	case "/sessions":
		ctx := context.Background()
		result := builtin.SessionListDirect(ctx)
		fmt.Println(result)

	case "/kill":
		builtin.ProcKillAll()
		fmt.Println("Non-MCP subprocesses killed.")

	case "/reset-terminal":
		fmt.Println("Run: stty sane")

	case "/reload":
		fmt.Println("Reload not yet implemented in v1.")

	case "/system":
		fmt.Println(r.sysCtx.Prompt())

	case "/doctor":
		r.runDoctor()

	case "/debug":
		if len(args) > 0 && args[0] == "messages" {
			fmt.Printf("Messages: %d\n", len(r.messages))
			for i, m := range r.messages {
				fmt.Printf("  [%d] %s: %d chars\n", i, m.Role, len(m.Content))
			}
		} else {
			fmt.Println("Usage: /debug messages")
		}

	case "/exit":
		r.running = false
		r.cleanup()
		fmt.Println("Goodbye.")
		os.Exit(0)

	default:
		fmt.Printf("Unknown command: %s (use /help)\n", cmd)
	}
}

func (r *REPL) runDoctor() {
	fmt.Println("Katty Doctor:")
	fmt.Println()

	// Config
	if r.cfg != nil {
		fmt.Println("Config: ok")
	} else {
		fmt.Println("Config: FAIL")
	}

	// API key
	if r.ds != nil && r.ds.APIKey() != "" {
		fmt.Println("DeepSeek API key: ok")
	} else {
		fmt.Println("DeepSeek API key: MISSING")
	}

	// Startup files
	loaded := 0
	for _, f := range r.sysCtx.StartupFiles {
		if f.Size > 0 {
			loaded++
		}
	}
	fmt.Printf("Startup files: %d loaded\n", loaded)

	// Environment probe
	fmt.Println("Environment probe: ok")

	// Capabilities
	if len(r.sysCtx.Capabilities) > 0 {
		families := 0
		for _, tools := range r.sysCtx.Capabilities {
			if len(tools) > 0 {
				families++
			}
		}
		fmt.Printf("Capabilities: ok, %d families scanned\n", families)
	} else {
		fmt.Println("Capabilities: none scanned")
	}

	// Built-ins
	fmt.Printf("Built-ins: ok, %d tools\n", r.builtins.Count())

	// proc.exec test
	ctx := context.Background()
	if bt := r.builtins.Get("katty.proc.exec"); bt != nil {
		result := bt.Handler(ctx, map[string]interface{}{
			"cmd":             "/bin/echo",
			"args":            []interface{}{"katty-test"},
			"timeout_seconds": float64(5),
		})
		if result.IsError {
			fmt.Printf("katty.proc.exec: FAIL (%s)\n", result.Error.Message)
		} else {
			fmt.Println("katty.proc.exec: ok")
		}
	} else {
		fmt.Println("katty.proc.exec: NOT REGISTERED")
	}

	// Targets
	if len(r.sysCtx.Targets) > 0 {
		fmt.Println("Targets: local ok")
	} else {
		fmt.Println("Targets: none")
	}

	// MCP
	if r.mcpMgr != nil {
		servers := r.mcpMgr.ListServers()
		for _, s := range servers {
			fmt.Printf("MCP %s: %s\n", s.Name, s.State)
		}
	}
}

func (r *REPL) cleanup() {
	if r.mcpMgr != nil {
		r.mcpMgr.StopAll(context.Background())
	}
	builtin.ProcKillAll()
	r.transcript.Close()
	signal.Reset(syscall.SIGINT)
}
