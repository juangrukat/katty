package systemctx

import (
	"fmt"
	"strings"

	"github.com/kat/katty/internal/config"
	"github.com/kat/katty/internal/envprobe"
	"github.com/kat/katty/internal/startup"
)

type CapabilityFamilies map[string][]string

type ToolDef struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Server      string `json:"server"`
}

type SystemContext struct {
	Env          envprobe.EnvInfo
	StartupFiles []startup.File
	Capabilities CapabilityFamilies
	Targets      map[string]config.Target
	BuiltinTools []ToolDef
	MCPServers   []MCPServerEntry
	MCPTools     []ToolDef
}

type MCPServerEntry struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

func (sc *SystemContext) Prompt() string {
	var b strings.Builder

	b.WriteString("You are Katty, Kat's local DeepSeek terminal assistant.\n\n")
	b.WriteString("You are a sharp local/remote systems workbench for Unix-like workflows.\n")
	b.WriteString("You are a toolmaking tool: you help Kat build, inspect, test, debug, and improve other tools.\n")
	b.WriteString("Be direct, practical, and grounded.\n")
	b.WriteString("Use the provided startup files, environment block, target list, capability scan, built-in tool list, MCP server list, conversation history, and tool results.\n")
	b.WriteString("When you need local or target information, use a Katty tool call instead of asking Kat to run shell commands.\n")
	b.WriteString("Never say you cannot access the machine if a relevant Katty tool, target tool, session tool, proc tool, fs tool, or MCP tool is available.\n")
	b.WriteString("Do not claim to have run tools unless a tool result is present.\n")
	b.WriteString("Prefer concrete shell-like steps.\n")
	b.WriteString("Prefer small composable tools.\n")
	b.WriteString("Detect the target environment before assuming commands, package managers, service managers, paths, kernel facilities, or userland behavior.\n")
	b.WriteString("Do not assume systemd, apt, GNU userland, or any specific distribution.\n\n")

	b.WriteString("Tool-call protocol:\n")
	b.WriteString("To call a tool, output only this exact block:\n\n")
	b.WriteString("<katty_tool_call>\n")
	b.WriteString(`{"server":"SERVER","tool":"TOOL","args":{...}}` + "\n")
	b.WriteString("</katty_tool_call>\n\n")
	b.WriteString("If you need multiple tools, output multiple katty_tool_call blocks.\n")
	b.WriteString("Do not wrap tool calls in markdown.\n")
	b.WriteString("Do not use XML attributes.\n")
	b.WriteString(`Do not invent <tool_calls> wrappers.` + "\n")
	b.WriteString(`Do not say "let me check" or "I'll run" without emitting a valid katty_tool_call.` + "\n\n")

	b.WriteString("Built-in tool guidance:\n")
	b.WriteString("Use katty.fs.* for filesystem work.\n")
	b.WriteString("Use katty.proc.* for local process work.\n")
	b.WriteString("Use katty.session.* for long-running or interactive processes.\n")
	b.WriteString("Use katty.target.* for remote machines.\n")
	b.WriteString("Use katty.os.* for environment and capability detection.\n")
	b.WriteString("Use katty.net.* for reachability checks.\n")
	b.WriteString("Use MCP only for configured specialized integrations.\n\n")

	// Startup files
	if len(sc.StartupFiles) > 0 {
		b.WriteString("<startup_files>\n")
		for _, f := range sc.StartupFiles {
			b.WriteString(f.Content)
			b.WriteString("\n")
		}
		b.WriteString("</startup_files>\n\n")
	}

	// Environment
	b.WriteString("<environment>\n")
	fmt.Fprintf(&b, "OS: %s\n", sc.Env.OS)
	fmt.Fprintf(&b, "Arch: %s\n", sc.Env.Arch)
	fmt.Fprintf(&b, "Uname: %s\n", sc.Env.Uname)
	fmt.Fprintf(&b, "User: %s\n", sc.Env.Username)
	fmt.Fprintf(&b, "Home: %s\n", sc.Env.Home)
	fmt.Fprintf(&b, "Shell: %s\n", sc.Env.Shell)
	fmt.Fprintf(&b, "CWD: %s\n", sc.Env.CWD)
	fmt.Fprintf(&b, "Tools found: %d\n", len(sc.Env.ToolPaths))
	b.WriteString("</environment>\n\n")

	// Capabilities
	b.WriteString("<capabilities>\n")
	for family, tools := range sc.Capabilities {
		if len(tools) > 0 {
			fmt.Fprintf(&b, "%s: %s\n", family, strings.Join(tools, ", "))
		}
	}
	b.WriteString("</capabilities>\n\n")

	// Targets
	b.WriteString("<targets>\n")
	for name, t := range sc.Targets {
		fmt.Fprintf(&b, "%s: type=%s", name, t.Type)
		if t.Default {
			b.WriteString(" (default)")
		}
		b.WriteString("\n")
	}
	b.WriteString("</targets>\n\n")

	// Built-in tools
	b.WriteString("<built_in_tools>\n")
	for _, t := range sc.BuiltinTools {
		fmt.Fprintf(&b, "%s: %s\n", t.Name, t.Description)
	}
	b.WriteString("</built_in_tools>\n\n")

	// MCP servers
	b.WriteString("<available_mcp_servers>\n")
	for _, s := range sc.MCPServers {
		fmt.Fprintf(&b, "%s: %s\n", s.Name, s.State)
	}
	b.WriteString("</available_mcp_servers>\n\n")

	// MCP tools
	if len(sc.MCPTools) > 0 {
		b.WriteString("<available_mcp_tools>\n")
		for _, t := range sc.MCPTools {
			fmt.Fprintf(&b, "%s: %s\n", t.Name, t.Description)
		}
		b.WriteString("</available_mcp_tools>\n\n")
	}

	return b.String()
}
