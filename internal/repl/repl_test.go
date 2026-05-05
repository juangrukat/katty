package repl

import (
	"testing"
)

func TestParseToolCalls_Single(t *testing.T) {
	text := `Let me check that.

<katty_tool_call>
{"server":"katty","tool":"katty.fs.list","args":{"path":"/tmp"}}
</katty_tool_call>`

	calls := parseToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Server != "katty" {
		t.Errorf("expected server=katty, got %s", calls[0].Server)
	}
	if calls[0].Tool != "katty.fs.list" {
		t.Errorf("expected tool=katty.fs.list, got %s", calls[0].Tool)
	}
	if calls[0].Args["path"] != "/tmp" {
		t.Errorf("expected path=/tmp, got %v", calls[0].Args["path"])
	}
}

func TestParseToolCalls_Multiple(t *testing.T) {
	text := `<katty_tool_call>
{"server":"katty","tool":"katty.fs.list","args":{"path":"/tmp"}}
</katty_tool_call>

<katty_tool_call>
{"server":"katty","tool":"katty.fs.read","args":{"path":"/tmp/test"}}
</katty_tool_call>`

	calls := parseToolCalls(text)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Tool != "katty.fs.list" {
		t.Errorf("first tool should be fs.list, got %s", calls[0].Tool)
	}
	if calls[1].Tool != "katty.fs.read" {
		t.Errorf("second tool should be fs.read, got %s", calls[1].Tool)
	}
}

func TestParseToolCalls_None(t *testing.T) {
	calls := parseToolCalls("Just a regular response, no tool calls.")
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
}

func TestParseToolCalls_MalformedJSON(t *testing.T) {
	text := `<katty_tool_call>
{not valid json}
</katty_tool_call>`

	calls := parseToolCalls(text)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls for malformed JSON, got %d", len(calls))
	}
}

func TestParseToolCalls_ShortToolName(t *testing.T) {
	// Model might emit short tool names like "fs.list" instead of "katty.fs.list"
	text := `<katty_tool_call>
{"server":"katty","tool":"fs.list","args":{"path":"/tmp"}}
</katty_tool_call>`

	calls := parseToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Tool != "fs.list" {
		t.Errorf("expected tool=fs.list, got %s", calls[0].Tool)
	}
}

func TestParseToolCalls_WithArgs(t *testing.T) {
	text := `<katty_tool_call>
{"server":"katty","tool":"katty.proc.exec","args":{"cmd":"/bin/echo","args":["hello"],"timeout_seconds":5}}
</katty_tool_call>`

	calls := parseToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if len(calls[0].Args) != 3 {
		t.Errorf("expected 3 args, got %d", len(calls[0].Args))
	}
}

// ── Dangling action tests ──

func TestIsDanglingAction_LetMeCheck(t *testing.T) {
	tests := []string{
		"Let me check that for you.",
		"I'll check the files now.",
		"Let me run that command.",
		"Let me try accessing that.",
		"Let me inspect the directory.",
		"Let me verify the result.",
		"I'll run that for you.",
		"checking the status now...",
	}

	for _, text := range tests {
		if !isDanglingAction(text) {
			t.Errorf("should be dangling: %s", text)
		}
	}
}

func TestIsDanglingAction_NotDangling(t *testing.T) {
	tests := []string{
		"Here are the files on your desktop:",
		"The command completed successfully.",
		"You have 3 files in that directory.",
		"I found the following results.",
	}

	for _, text := range tests {
		if isDanglingAction(text) {
			t.Errorf("should NOT be dangling: %s", text)
		}
	}
}

func TestIsDanglingAction_ColonEnding(t *testing.T) {
	// Ends with colon after announcing action
	if !isDanglingAction("Let me check that:") {
		t.Error("'Let me check that:' should be dangling")
	}
	if !isDanglingAction("I'll run the command:") {
		t.Error("'I'll run the command:' should be dangling")
	}
	// Colon but no action phrase
	if isDanglingAction("Here is the result:") {
		t.Error("'Here is the result:' should NOT be dangling")
	}
}

func TestParseToolCalls_MCPTool(t *testing.T) {
	text := `<katty_tool_call>
{"server":"file-utils","tool":"catalog_run_tool","args":{"name":"ls","positional":["-la"]}}
</katty_tool_call>`

	calls := parseToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Server != "file-utils" {
		t.Errorf("expected server=file-utils, got %s", calls[0].Server)
	}
}
