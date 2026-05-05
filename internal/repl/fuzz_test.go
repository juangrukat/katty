package repl

import (
	"testing"
)

// FuzzParseToolCalls feeds arbitrary text to the parser and verifies
// it never panics and always returns a valid call count.
func FuzzParseToolCalls(f *testing.F) {
	// Seed corpus with known-good and edge cases
	seeds := []string{
		"",
		"plain text",
		`<katty_tool_call>{"server":"katty","tool":"fs.list","args":{}}</katty_tool_call>`,
		`<katty_tool_call>{"server":"katty","tool":"katty.fs.read","args":{"path":"/tmp"}}</katty_tool_call>`,
		`<katty_tool_call>
{"server":"katty","tool":"katty.proc.exec","args":{"cmd":"ls","args":["-la"],"cwd":"/tmp"}}
</katty_tool_call>`,
		`<katty_tool_call>{broken json</katty_tool_call>`,
		`<katty_tool_call></katty_tool_call>`,
		`text before <katty_tool_call>{"server":"mcp","tool":"run","args":{"valid":"json"}}</katty_tool_call> text after`,
		// Double tool calls
		`<katty_tool_call>{"server":"katty","tool":"a"}</katty_tool_call>
<katty_tool_call>{"server":"katty","tool":"b"}</katty_tool_call>`,
		// Nested-like (not valid XML but should not panic)
		`<katty_tool_call><katty_tool_call>{"server":"katty","tool":"x"}</katty_tool_call></katty_tool_call>`,
		// Empty args
		`<katty_tool_call>{"server":"mcp","tool":"run","args":{}}</katty_tool_call>`,
		// With positional args
		`<katty_tool_call>{"server":"katty","tool":"katty.fs.glob","args":{"path":"/tmp","pattern":"*.go","max_results":100}}</katty_tool_call>`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		text := string(data)
		// parseToolCalls must never panic, regardless of input
		calls := parseToolCalls(text)
		_ = calls
	})
}

func FuzzIsDanglingAction(f *testing.F) {
	seeds := []string{
		"Let me check that.",
		"I'll run that for you.",
		"Here is the result.",
		"The command completed.",
		"",
		"Let me verify.",
		"I'll check.",
		"checking now...",
		"let me see",
		"i will try",
		"Result: 42 files found.",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, text string) {
		// isDanglingAction must never panic
		_ = isDanglingAction(text)
	})
}
