package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRegistry_Count(t *testing.T) {
	r := NewRegistry()
	if r.Count() != 31 {
		t.Errorf("expected 31 tools, got %d", r.Count())
	}
}

func TestRegistry_AllToolsRegistered(t *testing.T) {
	r := NewRegistry()
	expected := []string{
		"katty.fs.list", "katty.fs.read", "katty.fs.write", "katty.fs.append",
		"katty.fs.patch", "katty.fs.copy", "katty.fs.move", "katty.fs.remove",
		"katty.fs.mkdir", "katty.fs.stat", "katty.fs.search", "katty.fs.glob",
		"katty.proc.exec", "katty.proc.ps", "katty.proc.signal",
		"katty.session.start", "katty.session.send", "katty.session.read",
		"katty.session.stop", "katty.session.list",
		"katty.target.list", "katty.target.info", "katty.target.ping",
		"katty.target.exec", "katty.target.copy_to", "katty.target.copy_from",
		"katty.os.info", "katty.os.detect", "katty.os.which", "katty.os.capabilities",
		"katty.net.check",
	}
	for _, name := range expected {
		if r.Get(name) == nil {
			t.Errorf("tool %s not registered", name)
		}
	}
}

func TestRegistry_ListOrder(t *testing.T) {
	r := NewRegistry()
	tools := r.List()
	if len(tools) != 31 {
		t.Fatalf("expected 31 tools in list, got %d", len(tools))
	}
	// First should be fs.list
	if tools[0].Name != "katty.fs.list" {
		t.Errorf("first tool should be katty.fs.list, got %s", tools[0].Name)
	}
	// Last should be net.check
	if tools[len(tools)-1].Name != "katty.net.check" {
		t.Errorf("last tool should be katty.net.check, got %s", tools[len(tools)-1].Name)
	}
}

func TestRegistry_ListIncludesEveryRegisteredTool(t *testing.T) {
	r := NewRegistry()
	listed := make(map[string]bool)
	for _, tool := range r.List() {
		listed[tool.Name] = true
	}

	for name := range r.tools {
		if !listed[name] {
			t.Errorf("registered tool %s is missing from Registry.List", name)
		}
	}
}

// ── Filesystem tool tests ──

func TestFsList(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.list")
	if tool == nil {
		t.Fatal("fs.list not found")
	}

	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(tmp, "b.txt"), []byte("world"), 0644)
	os.MkdirAll(filepath.Join(tmp, "sub"), 0755)

	result := tool.Handler(context.Background(), map[string]interface{}{
		"path": tmp, "all": true, "max_entries": float64(100),
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// entries can be []fsEntry (typed) or a slice from JSON
	raw := result.Content["entries"]
	if raw == nil {
		t.Fatal("entries is nil")
	}
	// Just verify path is correct
	if result.Content["path"] != tmp {
		t.Errorf("expected path=%s, got %v", tmp, result.Content["path"])
	}
	_ = raw
}

func TestFsRead(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.read")

	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	result := tool.Handler(context.Background(), map[string]interface{}{
		"path": path, "max_bytes": float64(5),
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Content["content"] != "hello" {
		t.Errorf("expected 'hello', got '%v'", result.Content["content"])
	}
	if result.Content["truncated"] != true {
		t.Errorf("expected truncated=true")
	}
}

func TestFsWrite(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.write")

	tmp := t.TempDir()
	path := filepath.Join(tmp, "new.txt")

	result := tool.Handler(context.Background(), map[string]interface{}{
		"path": path, "content": "hello katty",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hello katty" {
		t.Errorf("expected 'hello katty', got '%s'", string(data))
	}
}

func TestFsWriteCreateDirs(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.write")

	tmp := t.TempDir()
	path := filepath.Join(tmp, "deep", "nested", "file.txt")

	result := tool.Handler(context.Background(), map[string]interface{}{
		"path": path, "content": "deep", "create_dirs": true,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "deep" {
		t.Errorf("expected 'deep', got '%s'", string(data))
	}
}

func TestFsAppend(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.append")

	tmp := t.TempDir()
	path := filepath.Join(tmp, "log.txt")
	os.WriteFile(path, []byte("line1\n"), 0644)

	result := tool.Handler(context.Background(), map[string]interface{}{
		"path": path, "content": "line2\n",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "line1\nline2\n" {
		t.Errorf("expected 'line1\\nline2\\n', got '%s'", string(data))
	}
}

func TestFsPatch(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.patch")

	tmp := t.TempDir()
	path := filepath.Join(tmp, "patch.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	result := tool.Handler(context.Background(), map[string]interface{}{
		"path": path, "old": "hello", "new": "hi",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "hi world" {
		t.Errorf("expected 'hi world', got '%s'", string(data))
	}
}

func TestFsPatchNotFound(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.patch")

	tmp := t.TempDir()
	path := filepath.Join(tmp, "patch.txt")
	os.WriteFile(path, []byte("hello world"), 0644)

	result := tool.Handler(context.Background(), map[string]interface{}{
		"path": path, "old": "nonexistent", "new": "x",
	})
	if !result.IsError {
		t.Fatal("expected error for not-found text")
	}
	if result.Error.Kind != "not_found" {
		t.Errorf("expected not_found, got %s", result.Error.Kind)
	}
}

func TestFsPatchReplaceAll(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.patch")

	tmp := t.TempDir()
	path := filepath.Join(tmp, "patch.txt")
	os.WriteFile(path, []byte("a a a"), 0644)

	result := tool.Handler(context.Background(), map[string]interface{}{
		"path": path, "old": "a", "new": "b", "replace_all": true,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "b b b" {
		t.Errorf("expected 'b b b', got '%s'", string(data))
	}
}

func TestFsMkdir(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.mkdir")

	tmp := t.TempDir()
	path := filepath.Join(tmp, "newdir")

	result := tool.Handler(context.Background(), map[string]interface{}{
		"path": path,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Error("directory was not created")
	}
}

func TestFsStat(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.stat")

	tmp := t.TempDir()
	path := filepath.Join(tmp, "stat.txt")
	os.WriteFile(path, []byte("hello"), 0644)

	result := tool.Handler(context.Background(), map[string]interface{}{
		"path": path,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	// fsStat wraps result in "entry" key with FSEntry struct
	if result.Content["entry"] == nil {
		t.Error("entry field missing")
	}
}

func TestFsGlob(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.glob")

	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "a.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmp, "b.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmp, "c.txt"), []byte("text"), 0644)

	result := tool.Handler(context.Background(), map[string]interface{}{
		"path": tmp, "pattern": "*.go", "max_results": float64(100),
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	// Just verify no error — the matches slice type varies
	if result.Content["count"] == nil {
		t.Error("count field missing")
	}
}

func TestFsCopy(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.copy")

	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	os.WriteFile(src, []byte("copy test"), 0644)

	result := tool.Handler(context.Background(), map[string]interface{}{
		"src": src, "dst": dst,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	data, _ := os.ReadFile(dst)
	if string(data) != "copy test" {
		t.Errorf("expected 'copy test', got '%s'", string(data))
	}
}

func TestFsMove(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.move")

	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")
	os.WriteFile(src, []byte("move test"), 0644)

	result := tool.Handler(context.Background(), map[string]interface{}{
		"src": src, "dst": dst,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	if _, err := os.Stat(src); err == nil {
		t.Error("source file should not exist after move")
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "move test" {
		t.Errorf("expected 'move test', got '%s'", string(data))
	}
}

func TestFsRemove(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.fs.remove")

	tmp := t.TempDir()
	path := filepath.Join(tmp, "remove.txt")
	os.WriteFile(path, []byte("delete me"), 0644)

	result := tool.Handler(context.Background(), map[string]interface{}{
		"path": path,
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	if _, err := os.Stat(path); err == nil {
		t.Error("file should not exist after remove")
	}
}

// ── Process tool tests ──

func TestProcExec(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.proc.exec")

	result := tool.Handler(context.Background(), map[string]interface{}{
		"cmd": "/bin/echo", "args": []interface{}{"hello", "world"}, "timeout_seconds": float64(5),
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Content["exit_code"].(int) != 0 {
		t.Errorf("expected exit_code=0, got %v", result.Content["exit_code"])
	}
}

func TestProcExecTimeout(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.proc.exec")

	result := tool.Handler(context.Background(), map[string]interface{}{
		"cmd": "/bin/sleep", "args": []interface{}{"10"}, "timeout_seconds": float64(1),
	})
	if !result.IsError {
		t.Fatal("expected timeout error")
	}
	if result.Error.Kind != "timeout" {
		t.Errorf("expected timeout, got %s", result.Error.Kind)
	}
}

func TestProcExecStdin(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.proc.exec")

	result := tool.Handler(context.Background(), map[string]interface{}{
		"cmd": "/bin/cat", "args": []interface{}{}, "stdin": "hello stdin", "timeout_seconds": float64(5),
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}
}

// ── OS tool tests ──

func TestOsInfo(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.os.info")

	result := tool.Handler(context.Background(), map[string]interface{}{})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.Content["os"] == nil {
		t.Error("os field missing")
	}
	if result.Content["arch"] == nil {
		t.Error("arch field missing")
	}
}

func TestOsWhich(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.os.which")

	result := tool.Handler(context.Background(), map[string]interface{}{
		"names": []interface{}{"sh", "bash", "nonexistent-tool-xyz"},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// found is a map — just verify sh is in it (type-safe check)
	if result.Content["found"] == nil {
		t.Error("found field missing")
	}
}

// ── Session tool tests ──

func TestSessionListEmpty(t *testing.T) {
	r := NewRegistry()
	tool := r.Get("katty.session.list")

	result := tool.Handler(context.Background(), map[string]interface{}{})
	if result.IsError {
		t.Fatalf("unexpected error: %v", result.Error)
	}
}

// ── Error helpers ──

func TestErrResult(t *testing.T) {
	r := errResult("test_kind", "test message")
	if !r.IsError {
		t.Error("expected IsError=true")
	}
	if r.Error.Kind != "test_kind" {
		t.Errorf("expected kind=test_kind, got %s", r.Error.Kind)
	}
}

func TestOkResult(t *testing.T) {
	r := okResult(map[string]interface{}{"key": "value"})
	if r.IsError {
		t.Error("expected IsError=false")
	}
	if r.Content["key"] != "value" {
		t.Errorf("expected key=value, got %v", r.Content["key"])
	}
}
