package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Benchmark tool execution latency

func BenchmarkFsList100(b *testing.B) {
	r := NewRegistry()
	tool := r.Get("katty.fs.list")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Handler(context.Background(), map[string]interface{}{
			"path":        "/tmp",
			"max_entries": float64(100),
		})
	}
}

func BenchmarkFsRead1KB(b *testing.B) {
	r := NewRegistry()
	tool := r.Get("katty.fs.read")
	tmp := b.TempDir()
	path := filepath.Join(tmp, "bench.txt")
	os.WriteFile(path, make([]byte, 1024), 0644)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Handler(context.Background(), map[string]interface{}{
			"path": path,
		})
	}
}

func BenchmarkFsStat(b *testing.B) {
	r := NewRegistry()
	tool := r.Get("katty.fs.stat")
	tmp := b.TempDir()
	path := filepath.Join(tmp, "stat_bench.txt")
	os.WriteFile(path, []byte("x"), 0644)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Handler(context.Background(), map[string]interface{}{
			"path": path,
		})
	}
}

func BenchmarkProcExecEcho(b *testing.B) {
	r := NewRegistry()
	tool := r.Get("katty.proc.exec")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Handler(context.Background(), map[string]interface{}{
			"cmd":  "/bin/echo",
			"args": []interface{}{"hello"},
		})
	}
}

func BenchmarkOsInfo(b *testing.B) {
	r := NewRegistry()
	tool := r.Get("katty.os.info")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Handler(context.Background(), map[string]interface{}{})
	}
}

func BenchmarkOsWhich(b *testing.B) {
	r := NewRegistry()
	tool := r.Get("katty.os.which")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tool.Handler(context.Background(), map[string]interface{}{
			"names": []interface{}{"sh", "bash", "go"},
		})
	}
}

// Benchmark registry lookups

func BenchmarkRegistryLookup(b *testing.B) {
	r := NewRegistry()
	names := []string{
		"katty.fs.list", "katty.fs.read", "katty.fs.write",
		"katty.proc.exec", "katty.os.info", "katty.os.which",
		"katty.fs.glob", "katty.fs.stat", "katty.fs.copy",
		"katty.fs.move", "katty.fs.remove", "katty.fs.mkdir",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.Get(names[i%len(names)])
	}
}

func BenchmarkToolRegistration(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewRegistry()
	}
}

// Benchmark tool call parsing (proxy from REPL)
func BenchmarkOkResult(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = okResult(map[string]interface{}{
			"key": "value",
			"num": 42,
		})
	}
}

func BenchmarkErrResult(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = errResult("test_kind", "test message")
	}
}
