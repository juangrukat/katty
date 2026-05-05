package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// FuzzFsPatch exercises find-and-replace on random inputs.
func FuzzFsPatch(f *testing.F) {
	seeds := []struct{ old, new, content string }{
		{"hello", "world", "hello world"},
		{"a", "b", "aaa"},
		{"x", "y", "no match here"},
		{"missing", "present", "some text"},
	}
	for _, s := range seeds {
		f.Add(s.old, s.new, s.content)
	}

	f.Fuzz(func(t *testing.T, oldStr, newStr, fileContent string) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "patch_fuzz.txt")
		os.WriteFile(path, []byte(fileContent), 0644)
		result := fsPatch(context.Background(), map[string]interface{}{
			"path":     path,
			"old_text": oldStr,
			"new_text": newStr,
		})
		_ = result
	})
}

// FuzzGetStr exercises type-assertion helper.
func FuzzGetStr(f *testing.F) {
	f.Add("hello")
	f.Add("")
	f.Fuzz(func(t *testing.T, s string) {
		args := map[string]interface{}{"key": s}
		result := getStr(args, "key")
		if result != s {
			t.Errorf("getStr roundtrip failed: %q != %q", result, s)
		}
	})
}

// FuzzGetInt exercises integer type coercion.
func FuzzGetInt(f *testing.F) {
	f.Add(int64(42))
	f.Add(int64(0))
	f.Add(int64(-1))
	f.Fuzz(func(t *testing.T, n int64) {
		args := map[string]interface{}{
			"key": float64(n), // JSON numbers come as float64
		}
		result := getInt(args, "key")
		expected := int(n)
		// Allow float64 truncation errors
		if n >= -9007199254740991 && n <= 9007199254740991 {
			if result != expected {
				t.Errorf("getInt: %d != %d", result, expected)
			}
		}
	})
}
