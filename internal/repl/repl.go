package repl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	state      replState
	mu         sync.Mutex
	outMu      sync.Mutex

	// State for Ctrl-C
	turnCancel context.CancelCauseFunc
	lastCtrlC  time.Time
}

type replState string

const (
	stateAwaitingUser     replState = "awaiting_user"
	stateAssistantRunning replState = "assistant_running"
	stateToolRunning      replState = "tool_running"
	stateDraining         replState = "draining"
)

var (
	ErrUserInterrupt = errors.New("user_interrupt")
	ErrSteering      = errors.New("steering")
)

type replEventKind int

const (
	eventInput replEventKind = iota
	eventInterrupt
	eventEOF
)

type replEvent struct {
	kind replEventKind
	line string
	err  error
}

type turnResult struct {
	cause       error
	baseMsgLen  int
	cancelledAt time.Time
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
		state:      stateAwaitingUser,
	}
}

func (r *REPL) Run() {
	events := make(chan replEvent, 16)
	shutdownCh := make(chan struct{})
	defer close(shutdownCh)

	go r.readInput(events, shutdownCh)

	// Set up signal handling
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT)

	go func() {
		for range sigCh {
			events <- replEvent{kind: eventInterrupt}
		}
	}()

	// Set system message
	sysPrompt := r.sysCtx.Prompt()
	r.messages = append(r.messages, deepseek.Message{Role: "system", Content: sysPrompt})

	r.println("Katty ready. Type /help for commands, /exit to quit.")
	r.println("")

	var turnDone chan turnResult
	var pendingSteer string

	r.printPrompt()

	for r.running {
		select {
		case ev := <-events:
			switch ev.kind {
			case eventEOF:
				if ev.err != nil && ev.err != io.EOF && r.running {
					r.fprintf(os.Stderr, "read error: %v\n", ev.err)
				}
				r.running = false
			case eventInterrupt:
				r.handleCtrlC()
				if r.getState() == stateAwaitingUser {
					r.printPrompt()
				}
			case eventInput:
				input := strings.TrimSpace(ev.line)
				if input == "" {
					if r.getState() == stateAwaitingUser {
						r.printPrompt()
					}
					continue
				}

				state := r.getState()
				if state == stateDraining {
					r.println("\nStill cancelling. Try again after the prompt returns.")
					continue
				}

				if state != stateAwaitingUser {
					if input == "/interrupt" {
						r.handleCtrlC()
					} else if strings.HasPrefix(input, "/") {
						r.println("\nOnly /interrupt and plain-text steering are available while a turn is running.")
					} else {
						pendingSteer = input
						r.setState(stateDraining)
						r.cancelTurn(ErrSteering)
						r.println("\nSteering received. Cancelling current turn...")
					}
					continue
				}

				if strings.HasPrefix(input, "/") {
					r.handleCommand(input)
					if r.running {
						r.printPrompt()
					}
					continue
				}

				if strings.HasPrefix(input, "!") {
					r.runShellPassthrough(input)
					if r.running {
						r.printPrompt()
					}
					continue
				}

				r.setState(stateAssistantRunning)
				turnDone = r.startTurn(input)
			}

		case result := <-turnDone:
			if result.cause != nil {
				r.rollbackMessages(result.baseMsgLen)
			}
			r.setState(stateAwaitingUser)
			turnDone = nil
			if pendingSteer != "" {
				next := pendingSteer
				pendingSteer = ""
				r.setState(stateAssistantRunning)
				turnDone = r.startTurn(next)
				continue
			}
			if r.running {
				r.printPrompt()
			}
		}
	}
}

func (r *REPL) readInput(events chan<- replEvent, shutdownCh <-chan struct{}) {
	for {
		select {
		case <-shutdownCh:
			return
		default:
		}

		input, err := r.reader.ReadString('\n')
		if err != nil {
			select {
			case events <- replEvent{kind: eventEOF, err: err}:
			case <-shutdownCh:
			}
			return
		}
		select {
		case events <- replEvent{kind: eventInput, line: input}:
		case <-shutdownCh:
			return
		}
	}
}

func (r *REPL) printPrompt() {
	r.print("> ")
}

func (r *REPL) print(args ...interface{}) {
	r.outMu.Lock()
	defer r.outMu.Unlock()
	fmt.Print(args...)
}

func (r *REPL) println(args ...interface{}) {
	r.outMu.Lock()
	defer r.outMu.Unlock()
	fmt.Println(args...)
}

func (r *REPL) fprintf(w io.Writer, format string, args ...interface{}) {
	r.outMu.Lock()
	defer r.outMu.Unlock()
	fmt.Fprintf(w, format, args...)
}

func (r *REPL) getState() replState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func (r *REPL) setState(state replState) {
	r.mu.Lock()
	r.state = state
	r.mu.Unlock()
}

func (r *REPL) startTurn(input string) chan turnResult {
	done := make(chan turnResult, 1)
	go func() {
		baseMsgLen := len(r.messages)
		r.transcript.Log(transcript.TypeUser, input, nil)
		r.messages = append(r.messages, deepseek.Message{Role: "user", Content: input})
		cause := r.runToolLoop(input)
		done <- turnResult{
			cause:       cause,
			baseMsgLen:  baseMsgLen,
			cancelledAt: time.Now(),
		}
	}()
	return done
}

func (r *REPL) rollbackMessages(length int) {
	if length < 0 || length > len(r.messages) {
		return
	}
	r.messages = r.messages[:length]
}

func (r *REPL) cancelTurn(cause error) {
	r.mu.Lock()
	if r.turnCancel != nil {
		r.turnCancel(cause)
		r.turnCancel = nil
	}
	r.mu.Unlock()
}

func (r *REPL) runShellPassthrough(input string) {
	cmdLine := strings.TrimSpace(strings.TrimPrefix(input, "!"))
	if cmdLine == "" {
		return
	}

	turnCtx, cancel := context.WithCancelCause(context.Background())
	r.mu.Lock()
	r.turnCancel = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.turnCancel = nil
		r.mu.Unlock()
		r.setState(stateAwaitingUser)
	}()

	r.setState(stateToolRunning)

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	cmd := exec.Command(shell, "-lc", cmdLine)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		r.fprintf(os.Stderr, "%v\n", err)
		return
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
	case <-turnCtx.Done():
		terminateShellProcessGroup(cmd, done)
	}
}

func terminateShellProcessGroup(cmd *exec.Cmd, done <-chan error) {
	if cmd.Process == nil {
		return
	}
	signalShellProcessGroup(cmd, syscall.SIGTERM)
	select {
	case <-done:
		return
	case <-time.After(2 * time.Second):
	}
	signalShellProcessGroup(cmd, syscall.SIGKILL)
	<-done
}

func signalShellProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		syscall.Kill(-pgid, sig)
	}
}

func (r *REPL) handleCtrlC() {
	now := time.Now()
	if now.Sub(r.lastCtrlC) < 2*time.Second {
		r.println("\nDouble Ctrl-C: exiting.")
		r.running = false
		os.Exit(0)
	}
	r.lastCtrlC = now

	if r.getState() != stateAwaitingUser {
		r.setState(stateDraining)
	}
	r.cancelTurn(ErrUserInterrupt)

	r.println("\nInterrupted. Returning to prompt.")
}

func (r *REPL) runToolLoop(input string) error {
	turnCtx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	r.mu.Lock()
	r.turnCancel = cancel
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.turnCancel = nil
		r.mu.Unlock()
	}()

	maxRounds := r.cfg.Tooling.MaxToolRounds
	if maxRounds <= 0 {
		maxRounds = 5
	}

	for round := 0; round < maxRounds; round++ {
		if turnCtx.Err() != nil {
			return context.Cause(turnCtx)
		}

		r.setState(stateAssistantRunning)
		resp, err := r.ds.Chat(turnCtx, r.messages)

		if err != nil {
			if turnCtx.Err() != nil {
				return context.Cause(turnCtx)
			}
			r.fprintf(os.Stderr, "DeepSeek error: %v\n", err)
			return nil
		}

		if len(resp.Choices) == 0 {
			r.println("(no response)")
			return nil
		}

		assistantMsg := resp.Choices[0].Message.Content
		if turnCtx.Err() != nil {
			return context.Cause(turnCtx)
		}
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

			r.println(assistantMsg)
			return nil
		}

		// Execute tools
		for _, tc := range toolCalls {
			if turnCtx.Err() != nil {
				return context.Cause(turnCtx)
			}
			r.setState(stateToolRunning)
			result := r.executeTool(turnCtx, tc)
			if turnCtx.Err() != nil {
				return context.Cause(turnCtx)
			}
			if r.cfg.Output.TerminalPassthrough && (tc.Final || isDisplayOnlyRequest(input)) {
				printed := r.printDisplayResult(result)
				if !printed && tc.Final {
					r.println(toolResultBody(result))
				}
				if printed || tc.Final {
					r.messages = append(r.messages, deepseek.Message{Role: "user", Content: tombstoneToolResult(tc)})
					return nil
				}
			}
			r.messages = append(r.messages, deepseek.Message{Role: "user", Content: result})
		}
	}

	r.println(fmt.Sprintf("Tool loop stopped after reaching max_tool_rounds=%d.", r.cfg.Tooling.MaxToolRounds))
	return nil
}

type toolCall struct {
	Server string                 `json:"server"`
	Tool   string                 `json:"tool"`
	Args   map[string]interface{} `json:"args"`
	Final  bool                   `json:"final,omitempty"`
}

type terminalOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
	HasExit  bool
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

func isDisplayOnlyRequest(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	if lower == "" {
		return false
	}

	agenticTerms := []string{
		"explain", "summarize", "summary", "interpret", "analyze",
		"diagnose", "debug", "fix", "why", "what does", "what is",
		"tell me about", "recommend", "change", "edit", "update",
		"modify", "repair", "investigate", "figure out",
	}
	for _, term := range agenticTerms {
		if strings.Contains(lower, term) {
			return false
		}
	}

	displayVerbs := []string{
		"show", "display", "print", "list", "read", "cat", "tail",
		"head", "grep", "search", "find", "run", "execute",
	}
	for _, verb := range displayVerbs {
		if lower == verb || strings.HasPrefix(lower, verb+" ") {
			return true
		}
	}

	terminalCommands := []string{
		"ls", "pwd", "date", "whoami", "which", "rg", "du", "df", "ps", "tree",
	}
	first := strings.Fields(lower)
	if len(first) == 0 {
		return false
	}
	for _, cmd := range terminalCommands {
		if first[0] == cmd {
			return true
		}
	}

	return strings.HasPrefix(lower, "what files") ||
		strings.HasPrefix(lower, "what is in ") ||
		strings.HasPrefix(lower, "what's in ")
}

func (r *REPL) executeTool(ctx context.Context, tc toolCall) string {
	start := time.Now()

	// Build fully qualified name: server.tool, but avoid double-prefixing
	// if the tool name is already fully qualified (e.g. "katty.fs.list")
	fullName := normalizeToolName(tc.Server, tc.Tool)

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

func normalizeToolName(server, tool string) string {
	if server == "katty" {
		switch tool {
		case "exec":
			return "katty.proc.exec"
		case "ps":
			return "katty.proc.ps"
		case "signal":
			return "katty.proc.signal"
		}
	}
	if strings.HasPrefix(tool, server+".") {
		return tool
	}
	return server + "." + tool
}

func (r *REPL) printDisplayResult(result string) bool {
	if r.printTerminalResult(result) {
		return true
	}
	return r.printStructuredDisplayResult(result)
}

func (r *REPL) printTerminalResult(result string) bool {
	body := toolResultBody(result)
	if body == "" {
		return false
	}

	output, ok := parseTerminalOutput(body)
	if !ok {
		return false
	}

	if output.Stdout != "" {
		r.print(output.Stdout)
		if !strings.HasSuffix(output.Stdout, "\n") {
			r.println("")
		}
	}
	if output.Stderr != "" {
		r.fprintf(os.Stderr, "%s", output.Stderr)
		if !strings.HasSuffix(output.Stderr, "\n") {
			r.fprintf(os.Stderr, "\n")
		}
	}
	if output.HasExit && output.ExitCode != 0 {
		r.printfExitCode(output.ExitCode)
	}
	return true
}

func (r *REPL) printStructuredDisplayResult(result string) bool {
	body := toolResultBody(result)
	if body == "" {
		return false
	}

	var content map[string]interface{}
	if err := json.Unmarshal([]byte(body), &content); err != nil {
		return false
	}

	if r.printFileReadDisplay(content) {
		return true
	}
	return r.printFileListDisplay(content)
}

func (r *REPL) printFileReadDisplay(content map[string]interface{}) bool {
	text, ok := renderFileReadDisplay(content)
	if !ok {
		return false
	}
	r.print(text)
	return true
}

func (r *REPL) printFileListDisplay(content map[string]interface{}) bool {
	text, ok := renderFileListDisplay(content)
	if !ok {
		return false
	}
	r.print(text)
	return true
}

func renderFileReadDisplay(content map[string]interface{}) (string, bool) {
	text, ok := content["content"].(string)
	if !ok {
		return "", false
	}
	var b strings.Builder
	b.WriteString(text)
	if !strings.HasSuffix(text, "\n") {
		b.WriteString("\n")
	}
	if truncated, _ := content["truncated"].(bool); truncated {
		b.WriteString("[truncated]\n")
	}
	return b.String(), true
}

func renderFileListDisplay(content map[string]interface{}) (string, bool) {
	rawEntries, ok := content["entries"].([]interface{})
	if !ok {
		return "", false
	}
	var b strings.Builder
	for _, raw := range rawEntries {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		if name == "" {
			continue
		}
		entryType, _ := entry["type"].(string)
		target, _ := entry["target"].(string)
		switch {
		case entryType == "dir":
			b.WriteString(name + "/\n")
		case entryType == "symlink" && target != "":
			b.WriteString(name + " -> " + target + "\n")
		default:
			b.WriteString(name + "\n")
		}
	}
	if truncated, _ := content["truncated"].(bool); truncated {
		b.WriteString("[truncated]\n")
	}
	return b.String(), true
}

func parseTerminalOutput(body string) (terminalOutput, bool) {
	if output, ok := parseJSONTerminalOutput(body); ok {
		return output, true
	}
	return parsePlainTerminalOutput(body)
}

func parseJSONTerminalOutput(body string) (terminalOutput, bool) {
	var content map[string]interface{}
	if err := json.Unmarshal([]byte(body), &content); err != nil {
		return terminalOutput{}, false
	}

	stdout, hasStdout := content["stdout"].(string)
	stderr, hasStderr := content["stderr"].(string)
	if !hasStdout && !hasStderr {
		return terminalOutput{}, false
	}

	output := terminalOutput{Stdout: stdout, Stderr: stderr}
	if exitCode, ok := numericValue(content["exit_code"]); ok {
		output.ExitCode = exitCode
		output.HasExit = true
	}
	return output, true
}

func parsePlainTerminalOutput(body string) (terminalOutput, bool) {
	lines := strings.Split(body, "\n")
	var output terminalOutput
	var section string
	var stdout, stderr strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "exit_code:"):
			if n, ok := parseExitCode(trimmed); ok {
				output.ExitCode = n
				output.HasExit = true
			}
			section = ""
		case trimmed == "stdout:":
			section = "stdout"
		case trimmed == "stderr:":
			section = "stderr"
		case section == "stdout":
			stdout.WriteString(line)
			stdout.WriteString("\n")
		case section == "stderr":
			stderr.WriteString(line)
			stderr.WriteString("\n")
		}
	}

	output.Stdout = normalizePlainStream(stdout.String())
	output.Stderr = normalizePlainStream(stderr.String())
	if output.Stdout == "" && output.Stderr == "" {
		return terminalOutput{}, false
	}
	return output, true
}

func normalizePlainStream(s string) string {
	if s == "" {
		return ""
	}
	return strings.TrimRight(s, "\n") + "\n"
}

func parseExitCode(line string) (int, bool) {
	value := strings.TrimSpace(strings.TrimPrefix(line, "exit_code:"))
	var n int
	if _, err := fmt.Sscanf(value, "%d", &n); err != nil {
		return 0, false
	}
	return n, true
}

func toolResultBody(result string) string {
	start := strings.Index(result, ">")
	end := strings.LastIndex(result, "</tool_result>")
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return strings.TrimSpace(result[start+1 : end])
}

func tombstoneToolResult(tc toolCall) string {
	return fmt.Sprintf(`<tool_result server="%s" tool="%s">
[output shown to user]
</tool_result>`, tc.Server, tc.Tool)
}

func numericValue(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	default:
		return 0, false
	}
}

func (r *REPL) printfExitCode(exitCode int) {
	r.println("exit_code:", exitCode)
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
