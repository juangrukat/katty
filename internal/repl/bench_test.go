package repl

import "testing"

func BenchmarkParseToolCalls_1Call(b *testing.B) {
	text := `Let me check that for you.

<katty_tool_call>
{"server":"katty","tool":"katty.fs.list","args":{"path":"/tmp/test","max_entries":100}}
</katty_tool_call>`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseToolCalls(text)
	}
}

func BenchmarkParseToolCalls_3Calls(b *testing.B) {
	text := `<katty_tool_call>
{"server":"katty","tool":"katty.fs.list","args":{"path":"/tmp"}}
</katty_tool_call>

<katty_tool_call>
{"server":"katty","tool":"katty.fs.read","args":{"path":"/tmp/a"}}
</katty_tool_call>

<katty_tool_call>
{"server":"katty","tool":"katty.fs.stat","args":{"path":"/tmp/b"}}
</katty_tool_call>`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseToolCalls(text)
	}
}

func BenchmarkIsDanglingAction(b *testing.B) {
	texts := []string{
		"Let me check that for you.",
		"I'll run that command now.",
		"Here are the results:",
		"The command completed successfully.",
		"Let me verify the output.",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = isDanglingAction(texts[i%len(texts)])
	}
}
