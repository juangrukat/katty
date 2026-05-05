package builtin

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type FSEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Mode     string `json:"mode"`
	Modified string `json:"modified"`
	Target   string `json:"target,omitempty"`
}

func registerFS(r *Registry) {
	// katty.fs.list
	r.register(&Tool{
		Name:        "katty.fs.list",
		Description: "List directory contents",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":        map[string]interface{}{"type": "string", "description": "Directory path"},
				"all":         map[string]interface{}{"type": "boolean", "description": "Include hidden files"},
				"long":        map[string]interface{}{"type": "boolean", "description": "Detailed listing"},
				"recursive":   map[string]interface{}{"type": "boolean", "description": "Recurse into subdirectories"},
				"max_entries": map[string]interface{}{"type": "integer", "description": "Max entries to return"},
			},
			"required": []string{"path"},
		},
		Handler: fsList,
	})

	// katty.fs.read
	r.register(&Tool{
		Name:        "katty.fs.read",
		Description: "Read file contents",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":      map[string]interface{}{"type": "string", "description": "File path"},
				"offset":    map[string]interface{}{"type": "integer", "description": "Byte offset to start reading"},
				"max_bytes": map[string]interface{}{"type": "integer", "description": "Maximum bytes to read"},
			},
			"required": []string{"path"},
		},
		Handler: fsRead,
	})

	// katty.fs.write
	r.register(&Tool{
		Name:        "katty.fs.write",
		Description: "Write file contents (overwrite)",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":        map[string]interface{}{"type": "string", "description": "File path"},
				"content":     map[string]interface{}{"type": "string", "description": "Content to write"},
				"create_dirs": map[string]interface{}{"type": "boolean", "description": "Create parent directories"},
				"mode":        map[string]interface{}{"type": "string", "description": "File mode (e.g. 0644)"},
			},
			"required": []string{"path", "content"},
		},
		Handler: fsWrite,
	})

	// katty.fs.append
	r.register(&Tool{
		Name:        "katty.fs.append",
		Description: "Append content to a file",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "File path"},
				"content": map[string]interface{}{"type": "string", "description": "Content to append"},
				"create":  map[string]interface{}{"type": "boolean", "description": "Create file if missing"},
			},
			"required": []string{"path", "content"},
		},
		Handler: fsAppend,
	})

	// katty.fs.patch
	r.register(&Tool{
		Name:        "katty.fs.patch",
		Description: "Replace exact text in a file",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":        map[string]interface{}{"type": "string", "description": "File path"},
				"old":         map[string]interface{}{"type": "string", "description": "Exact text to find"},
				"new":         map[string]interface{}{"type": "string", "description": "Replacement text"},
				"replace_all": map[string]interface{}{"type": "boolean", "description": "Replace all occurrences"},
			},
			"required": []string{"path", "old", "new"},
		},
		Handler: fsPatch,
	})

	// katty.fs.copy
	r.register(&Tool{
		Name:        "katty.fs.copy",
		Description: "Copy a file or directory",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"src":       map[string]interface{}{"type": "string", "description": "Source path"},
				"dst":       map[string]interface{}{"type": "string", "description": "Destination path"},
				"recursive": map[string]interface{}{"type": "boolean", "description": "Copy directories recursively"},
				"overwrite": map[string]interface{}{"type": "boolean", "description": "Overwrite destination"},
			},
			"required": []string{"src", "dst"},
		},
		Handler: fsCopy,
	})

	// katty.fs.move
	r.register(&Tool{
		Name:        "katty.fs.move",
		Description: "Move/rename a file or directory",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"src":       map[string]interface{}{"type": "string", "description": "Source path"},
				"dst":       map[string]interface{}{"type": "string", "description": "Destination path"},
				"overwrite": map[string]interface{}{"type": "boolean", "description": "Overwrite destination"},
			},
			"required": []string{"src", "dst"},
		},
		Handler: fsMove,
	})

	// katty.fs.remove
	r.register(&Tool{
		Name:        "katty.fs.remove",
		Description: "Remove a file or directory",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":      map[string]interface{}{"type": "string", "description": "Path to remove"},
				"recursive": map[string]interface{}{"type": "boolean", "description": "Remove directories recursively"},
			},
			"required": []string{"path"},
		},
		Handler: fsRemove,
	})

	// katty.fs.mkdir
	r.register(&Tool{
		Name:        "katty.fs.mkdir",
		Description: "Create a directory",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "Directory path"},
				"parents": map[string]interface{}{"type": "boolean", "description": "Create parent directories"},
			},
			"required": []string{"path"},
		},
		Handler: fsMkdir,
	})

	// katty.fs.stat
	r.register(&Tool{
		Name:        "katty.fs.stat",
		Description: "Get file or directory info",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "Path to stat"},
			},
			"required": []string{"path"},
		},
		Handler: fsStat,
	})

	// katty.fs.search
	r.register(&Tool{
		Name:        "katty.fs.search",
		Description: "Search file contents (uses rg if available)",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":        map[string]interface{}{"type": "string", "description": "Directory to search in"},
				"query":       map[string]interface{}{"type": "string", "description": "Text or regex to search for"},
				"glob":        map[string]interface{}{"type": "string", "description": "File glob pattern (e.g., *.go)"},
				"max_results": map[string]interface{}{"type": "integer", "description": "Maximum results"},
			},
			"required": []string{"path", "query"},
		},
		Handler: fsSearch,
	})

	// katty.fs.glob
	r.register(&Tool{
		Name:        "katty.fs.glob",
		Description: "Find files matching a glob pattern",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":        map[string]interface{}{"type": "string", "description": "Root directory"},
				"pattern":     map[string]interface{}{"type": "string", "description": "Glob pattern (e.g., **/*.go)"},
				"max_results": map[string]interface{}{"type": "integer", "description": "Maximum results"},
			},
			"required": []string{"path", "pattern"},
		},
		Handler: fsGlob,
	})
}

func fsList(ctx context.Context, args map[string]interface{}) ToolResult {
	path := getStr(args, "path")
	if path == "" {
		return errResult("invalid_args", "path is required")
	}
	showAll := getBool(args, "all")
	long := getBool(args, "long")
	recursive := getBool(args, "recursive")
	maxEntries := getInt(args, "max_entries")
	if maxEntries <= 0 {
		maxEntries = 500
	}

	var entries []FSEntry
	truncated := false

	walkFn := func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if !showAll && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() && p != path {
				return filepath.SkipDir
			}
			return nil
		}
		if len(entries) >= maxEntries {
			truncated = true
			return filepath.SkipAll
		}

		entry := FSEntry{
			Name: d.Name(),
			Path: p,
		}
		info, err := d.Info()
		if err == nil {
			entry.Size = info.Size()
			entry.Mode = info.Mode().String()
			entry.Modified = info.ModTime().Format(time.RFC3339)
		}
		switch {
		case d.IsDir():
			entry.Type = "dir"
		case d.Type()&fs.ModeSymlink != 0:
			entry.Type = "symlink"
			if target, err := os.Readlink(p); err == nil {
				entry.Target = target
			}
		default:
			entry.Type = "file"
		}
		entries = append(entries, entry)

		if !recursive && d.IsDir() && p != path {
			return filepath.SkipDir
		}
		return nil
	}

	filepath.WalkDir(path, walkFn)

	result := map[string]interface{}{
		"path":    path,
		"entries": entries,
	}
	if truncated {
		result["truncated"] = true
	}
	if long {
		result["total"] = len(entries)
	}
	return okResult(result)
}

func fsRead(ctx context.Context, args map[string]interface{}) ToolResult {
	path := getStr(args, "path")
	if path == "" {
		return errResult("invalid_args", "path is required")
	}
	offset := getInt(args, "offset")
	maxBytes := getInt(args, "max_bytes")
	if maxBytes <= 0 {
		maxBytes = 20000
	}

	f, err := os.Open(path)
	if err != nil {
		return errResult("file_error", err.Error())
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return errResult("file_error", err.Error())
	}

	if offset > 0 {
		if _, err := f.Seek(int64(offset), io.SeekStart); err != nil {
			return errResult("file_error", fmt.Sprintf("seek: %v", err))
		}
	}

	buf := make([]byte, maxBytes)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return errResult("file_error", err.Error())
	}

	truncated := n == maxBytes && info.Size()-int64(offset) > int64(maxBytes)

	return okResult(map[string]interface{}{
		"path":       path,
		"size":       info.Size(),
		"offset":     offset,
		"bytes_read": n,
		"content":    string(buf[:n]),
		"truncated":  truncated,
	})
}

func fsWrite(ctx context.Context, args map[string]interface{}) ToolResult {
	path := getStr(args, "path")
	content := getStr(args, "content")
	if path == "" {
		return errResult("invalid_args", "path is required")
	}
	createDirs := getBool(args, "create_dirs")
	modeStr := getStr(args, "mode")

	if createDirs {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return errResult("file_error", fmt.Sprintf("mkdir: %v", err))
		}
	}

	var mode os.FileMode = 0644
	if modeStr != "" {
		if m, err := parseMode(modeStr); err == nil {
			mode = m
		}
	}

	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		return errResult("file_error", err.Error())
	}

	return okResult(map[string]interface{}{
		"path":          path,
		"bytes_written": len(content),
	})
}

func fsAppend(ctx context.Context, args map[string]interface{}) ToolResult {
	path := getStr(args, "path")
	content := getStr(args, "content")
	create := getBool(args, "create")

	flag := os.O_APPEND | os.O_WRONLY
	if create {
		flag |= os.O_CREATE
	}

	f, err := os.OpenFile(path, flag, 0644)
	if err != nil {
		return errResult("file_error", err.Error())
	}
	defer f.Close()

	n, err := f.WriteString(content)
	if err != nil {
		return errResult("file_error", err.Error())
	}

	return okResult(map[string]interface{}{
		"path":          path,
		"bytes_written": n,
	})
}

func fsPatch(ctx context.Context, args map[string]interface{}) ToolResult {
	path := getStr(args, "path")
	old := getStr(args, "old")
	new := getStr(args, "new")
	replaceAll := getBool(args, "replace_all")

	if path == "" || old == "" {
		return errResult("invalid_args", "path and old are required")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return errResult("file_error", err.Error())
	}

	text := string(data)
	count := strings.Count(text, old)
	if count == 0 {
		return errResult("not_found", "old text not found in file")
	}

	var result string
	replacements := 1
	if replaceAll {
		result = strings.ReplaceAll(text, old, new)
		replacements = count
	} else {
		result = strings.Replace(text, old, new, 1)
	}

	if err := os.WriteFile(path, []byte(result), 0644); err != nil {
		return errResult("file_error", err.Error())
	}

	return okResult(map[string]interface{}{
		"path":         path,
		"replacements": replacements,
		"size":         len(result),
	})
}

func fsCopy(ctx context.Context, args map[string]interface{}) ToolResult {
	src := getStr(args, "src")
	dst := getStr(args, "dst")
	recursive := getBool(args, "recursive")
	overwrite := getBool(args, "overwrite")

	if src == "" || dst == "" {
		return errResult("invalid_args", "src and dst are required")
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return errResult("file_error", err.Error())
	}

	if srcInfo.IsDir() {
		if !recursive {
			return errResult("invalid_args", "cannot copy directory without recursive=true")
		}
		return copyDir(src, dst, overwrite)
	}

	if !overwrite {
		if _, err := os.Stat(dst); err == nil {
			return errResult("file_exists", "destination exists and overwrite is false")
		}
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return errResult("file_error", err.Error())
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return errResult("file_error", err.Error())
	}
	defer dstFile.Close()

	n, err := io.Copy(dstFile, srcFile)
	if err != nil {
		return errResult("file_error", err.Error())
	}

	return okResult(map[string]interface{}{
		"src":   src,
		"dst":   dst,
		"bytes": n,
	})
}

func copyDir(src, dst string, overwrite bool) ToolResult {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return errResult("file_error", err.Error())
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return errResult("file_error", err.Error())
	}

	copied := 0
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			res := copyDir(srcPath, dstPath, overwrite)
			if res.IsError {
				return res
			}
			if c, ok := res.Content["copied"].(float64); ok {
				copied += int(c)
			}
		} else {
			srcFile, err := os.Open(srcPath)
			if err != nil {
				continue
			}
			flag := os.O_CREATE | os.O_WRONLY
			if overwrite {
				flag |= os.O_TRUNC
			} else {
				flag |= os.O_EXCL
			}
			dstFile, err := os.OpenFile(dstPath, flag, 0644)
			if err != nil {
				srcFile.Close()
				continue
			}
			io.Copy(dstFile, srcFile)
			srcFile.Close()
			dstFile.Close()
			copied++
		}
	}

	return okResult(map[string]interface{}{
		"src":    src,
		"dst":    dst,
		"copied": copied,
	})
}

func fsMove(ctx context.Context, args map[string]interface{}) ToolResult {
	src := getStr(args, "src")
	dst := getStr(args, "dst")
	overwrite := getBool(args, "overwrite")

	if src == "" || dst == "" {
		return errResult("invalid_args", "src and dst are required")
	}

	if !overwrite {
		if _, err := os.Stat(dst); err == nil {
			return errResult("file_exists", "destination exists and overwrite is false")
		}
	}

	if err := os.Rename(src, dst); err != nil {
		return errResult("file_error", err.Error())
	}

	return okResult(map[string]interface{}{
		"src": src,
		"dst": dst,
	})
}

func fsRemove(ctx context.Context, args map[string]interface{}) ToolResult {
	path := getStr(args, "path")
	recursive := getBool(args, "recursive")

	if path == "" {
		return errResult("invalid_args", "path is required")
	}

	info, err := os.Stat(path)
	if err != nil {
		return errResult("file_error", err.Error())
	}

	if info.IsDir() && !recursive {
		return errResult("invalid_args", "cannot remove directory without recursive=true")
	}

	if recursive {
		err = os.RemoveAll(path)
	} else {
		err = os.Remove(path)
	}

	if err != nil {
		return errResult("file_error", err.Error())
	}

	return okResult(map[string]interface{}{
		"path":    path,
		"removed": true,
	})
}

func fsMkdir(ctx context.Context, args map[string]interface{}) ToolResult {
	path := getStr(args, "path")
	parents := getBool(args, "parents")

	var err error
	if parents {
		err = os.MkdirAll(path, 0755)
	} else {
		err = os.Mkdir(path, 0755)
	}
	if err != nil {
		return errResult("file_error", err.Error())
	}

	return okResult(map[string]interface{}{
		"path":    path,
		"created": true,
	})
}

func fsStat(ctx context.Context, args map[string]interface{}) ToolResult {
	path := getStr(args, "path")
	if path == "" {
		return errResult("invalid_args", "path is required")
	}

	info, err := os.Stat(path)
	if err != nil {
		return errResult("file_error", err.Error())
	}

	entry := FSEntry{
		Name:     info.Name(),
		Path:     path,
		Size:     info.Size(),
		Mode:     info.Mode().String(),
		Modified: info.ModTime().Format(time.RFC3339),
	}

	if info.IsDir() {
		entry.Type = "dir"
	} else if info.Mode()&fs.ModeSymlink != 0 {
		entry.Type = "symlink"
		if target, err := os.Readlink(path); err == nil {
			entry.Target = target
		}
	} else {
		entry.Type = "file"
	}

	return okResult(map[string]interface{}{
		"entry": entry,
	})
}

func fsSearch(ctx context.Context, args map[string]interface{}) ToolResult {
	path := getStr(args, "path")
	query := getStr(args, "query")
	glob := getStr(args, "glob")
	maxResults := getInt(args, "max_results")
	if maxResults <= 0 {
		maxResults = 200
	}

	// Try rg first
	if _, err := exec.LookPath("rg"); err == nil {
		return fsSearchRg(path, query, glob, maxResults)
	}

	// Fallback to Go
	return fsSearchGo(path, query, glob, maxResults)
}

type searchResult struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

func fsSearchRg(path, query, glob string, maxResults int) ToolResult {
	args := []string{"--line-number", "--no-heading", "--color", "never"}
	if glob != "" {
		args = append(args, "--glob", glob)
	}
	args = append(args, "-m", fmt.Sprintf("%d", maxResults))
	args = append(args, query, path)

	cmd := exec.Command("rg", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			// No matches
			return okResult(map[string]interface{}{
				"path":    path,
				"query":   query,
				"results": []searchResult{},
				"count":   0,
			})
		}
		return errResult("search_error", err.Error())
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var results []searchResult
	for _, line := range lines {
		if len(results) >= maxResults {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		file := parts[0]
		rest := parts[1]
		parts2 := strings.SplitN(rest, ":", 2)
		if len(parts2) < 2 {
			continue
		}
		lineNum := 0
		fmt.Sscanf(parts2[0], "%d", &lineNum)
		results = append(results, searchResult{
			File:    file,
			Line:    lineNum,
			Content: strings.TrimSpace(parts2[1]),
		})
	}

	return okResult(map[string]interface{}{
		"path":    path,
		"query":   query,
		"results": results,
		"count":   len(results),
		"engine":  "rg",
	})
}

func fsSearchGo(root, query, glob string, maxResults int) ToolResult {
	var results []searchResult

	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if glob != "" {
			matched, err := filepath.Match(glob, d.Name())
			if err != nil || !matched {
				return nil
			}
		}
		if len(results) >= maxResults {
			return filepath.SkipAll
		}

		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if len(results) >= maxResults {
				break
			}
			if strings.Contains(line, query) {
				results = append(results, searchResult{
					File:    p,
					Line:    i + 1,
					Content: strings.TrimSpace(line),
				})
			}
		}
		return nil
	})

	return okResult(map[string]interface{}{
		"path":    root,
		"query":   query,
		"results": results,
		"count":   len(results),
		"engine":  "go",
	})
}

func fsGlob(ctx context.Context, args map[string]interface{}) ToolResult {
	root := getStr(args, "path")
	pattern := getStr(args, "pattern")
	maxResults := getInt(args, "max_results")
	if maxResults <= 0 {
		maxResults = 500
	}

	var matches []string
	fullPattern := filepath.Join(root, pattern)

	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if len(matches) >= maxResults {
			return filepath.SkipAll
		}
		matched, _ := filepath.Match(fullPattern, p)
		if !matched {
			// Try simpler matching
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return nil
			}
			matched, _ = filepath.Match(pattern, rel)
		}
		if matched {
			matches = append(matches, p)
		}
		return nil
	})

	truncated := len(matches) >= maxResults
	if truncated {
		matches = matches[:maxResults]
	}

	result := map[string]interface{}{
		"path":    root,
		"pattern": pattern,
		"matches": matches,
		"count":   len(matches),
	}
	if truncated {
		result["truncated"] = true
	}
	return okResult(result)
}

func parseMode(s string) (os.FileMode, error) {
	var m uint32
	_, err := fmt.Sscanf(s, "%o", &m)
	if err != nil {
		return 0, err
	}
	return os.FileMode(m), nil
}
