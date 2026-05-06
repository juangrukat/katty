package repl

import (
	"encoding/json"
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

func TestParseToolCalls_Final(t *testing.T) {
	text := `<katty_tool_call>
{"server":"katty","tool":"katty.proc.exec","final":true,"args":{"cmd":"/bin/ls","args":["-la"]}}
</katty_tool_call>`

	calls := parseToolCalls(text)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if !calls[0].Final {
		t.Fatal("expected final=true")
	}
}

func TestIsDisplayOnlyRequest(t *testing.T) {
	tests := []string{
		"list the files on my desktop",
		"show me /tmp",
		"display the current config",
		"print the first ten lines",
		"run go test ./...",
		"pwd",
		"what files are on my desktop",
	}

	for _, text := range tests {
		if !isDisplayOnlyRequest(text) {
			t.Errorf("should be display-only request: %s", text)
		}
	}
}

func TestIsDisplayOnlyRequest_AgenticIntent(t *testing.T) {
	tests := []string{
		"list the files and summarize them",
		"run go test ./... and fix failures",
		"show the logs and diagnose the problem",
		"find the bug and repair it",
	}

	for _, text := range tests {
		if isDisplayOnlyRequest(text) {
			t.Errorf("should not be display-only request: %s", text)
		}
	}
}

func TestParseTerminalOutput_JSON(t *testing.T) {
	body := `{"stdout":"hello\n","stderr":"","exit_code":0}`

	output, ok := parseTerminalOutput(body)
	if !ok {
		t.Fatal("expected JSON terminal output")
	}
	if output.Stdout != "hello\n" {
		t.Fatalf("stdout mismatch: %q", output.Stdout)
	}
	if !output.HasExit || output.ExitCode != 0 {
		t.Fatalf("exit code mismatch: %#v", output)
	}
}

func TestParseTerminalOutput_PlainToolResult(t *testing.T) {
	body := `exit_code: 0

stdout:
total 8
drwxr-xr-x  2 kat staff 64 May  6 .
-rw-r--r--  1 kat staff 12 May  6 note.txt

`

	output, ok := parseTerminalOutput(body)
	if !ok {
		t.Fatal("expected plain terminal output")
	}
	if !output.HasExit || output.ExitCode != 0 {
		t.Fatalf("exit code mismatch: %#v", output)
	}
	if want := "total 8\ndrwxr-xr-x  2 kat staff 64 May  6 .\n-rw-r--r--  1 kat staff 12 May  6 note.txt\n"; output.Stdout != want {
		t.Fatalf("stdout mismatch:\nwant %q\ngot  %q", want, output.Stdout)
	}
}

func TestRenderFileListDisplay(t *testing.T) {
	result := `<tool_result server="katty" tool="katty.fs.list" elapsed_ms="1">
{"entries":[{"name":"alpha","type":"dir"},{"name":"note.txt","type":"file"},{"name":"link","type":"symlink","target":"/tmp/target"}],"path":"/tmp","truncated":true}
</tool_result>`

	body := toolResultBody(result)
	var content map[string]interface{}
	if err := json.Unmarshal([]byte(body), &content); err != nil {
		t.Fatalf("json parse: %v", err)
	}
	got, ok := renderFileListDisplay(content)
	if !ok {
		t.Fatal("expected file list display")
	}
	want := "alpha/\nnote.txt\nlink -> /tmp/target\n[truncated]\n"
	if got != want {
		t.Fatalf("display mismatch:\nwant %q\ngot  %q", want, got)
	}
}
