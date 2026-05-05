package builtin

import (
	"context"
)

// ToolResult is the normalized result from any tool call.
type ToolResult struct {
	IsError bool                   `json:"-"`
	Error   *ToolError             `json:"error,omitempty"`
	Content map[string]interface{} `json:"content,omitempty"`
	Text    string                 `json:"text,omitempty"`
}

type ToolError struct {
	Kind    string `json:"error_kind"`
	Message string `json:"message"`
}

// Tool defines a callable built-in tool.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]interface{}
	Handler     func(ctx context.Context, args map[string]interface{}) ToolResult
}

// Registry holds all built-in tools.
type Registry struct {
	tools map[string]*Tool
}

func NewRegistry() *Registry {
	r := &Registry{tools: make(map[string]*Tool)}
	r.registerAll()
	return r
}

func (r *Registry) register(t *Tool) {
	r.tools[t.Name] = t
}

func (r *Registry) Get(name string) *Tool {
	return r.tools[name]
}

func (r *Registry) List() []*Tool {
	var tools []*Tool
	// Return in a stable order
	order := []string{
		// fs
		"katty.fs.list", "katty.fs.read", "katty.fs.write", "katty.fs.append",
		"katty.fs.patch", "katty.fs.copy", "katty.fs.move", "katty.fs.remove",
		"katty.fs.mkdir", "katty.fs.stat", "katty.fs.search", "katty.fs.glob",
		// proc
		"katty.proc.exec", "katty.proc.ps", "katty.proc.signal",
		// session
		"katty.session.start", "katty.session.send", "katty.session.read",
		"katty.session.stop", "katty.session.list",
		// target
		"katty.target.list", "katty.target.info", "katty.target.ping",
		"katty.target.exec", "katty.target.copy_to", "katty.target.copy_from",
		// os
		"katty.os.info", "katty.os.detect", "katty.os.which", "katty.os.capabilities",
		// net
		"katty.net.check",
	}
	for _, name := range order {
		if t, ok := r.tools[name]; ok {
			tools = append(tools, t)
		}
	}
	return tools
}

func (r *Registry) Count() int {
	return len(r.tools)
}

// registerAll registers all built-in tool domains.
func (r *Registry) registerAll() {
	registerFS(r)
	registerProc(r)
	registerSession(r)
	registerTarget(r)
	registerOS(r)
	registerNet(r)
}

func errResult(kind, msg string) ToolResult {
	return ToolResult{
		IsError: true,
		Error:   &ToolError{Kind: kind, Message: msg},
	}
}

func okResult(data map[string]interface{}) ToolResult {
	return ToolResult{Content: data}
}

func getStr(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getBool(args map[string]interface{}, key string) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getInt(args map[string]interface{}, key string) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}

func getStrList(args map[string]interface{}, key string) []string {
	if v, ok := args[key]; ok {
		if arr, ok := v.([]interface{}); ok {
			result := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
	}
	return nil
}
