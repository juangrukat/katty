package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/kat/katty/internal/config"
)

type ServerState string

const (
	StateDisabled ServerState = "disabled"
	StateStarting ServerState = "starting"
	StateRunning  ServerState = "running"
	StateFailed   ServerState = "failed"
	StateStopped  ServerState = "stopped"
)

type ServerStatus struct {
	Name            string      `json:"name"`
	State           ServerState `json:"state"`
	Tools           []ToolInfo  `json:"tools,omitempty"`
	InitError       string      `json:"init_error,omitempty"`
	CachedToolsUsed bool        `json:"cached_tools_used"`
	StartedAt       time.Time   `json:"started_at,omitempty"`
	LastSeen        time.Time   `json:"last_seen,omitempty"`
}

type ToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Schema      map[string]interface{} `json:"inputSchema"`
	Server      string                 `json:"server"`
}

type ToolResult struct {
	Content []ContentItem          `json:"content,omitempty"`
	Result  map[string]interface{} `json:"result,omitempty"`
	IsError bool                   `json:"isError,omitempty"`
}

type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Manager manages MCP server processes.
type Manager struct {
	mu      sync.Mutex
	servers map[string]*serverState
	cfg     map[string]config.MCPServerConfig
}

type serverState struct {
	config    config.MCPServerConfig
	status    ServerState
	client    *StdioClient
	tools     []ToolInfo
	initError string
}

func NewManager(cfg map[string]config.MCPServerConfig) *Manager {
	return &Manager{
		servers: make(map[string]*serverState),
		cfg:     cfg,
	}
}

func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Start all enabled servers concurrently
	var wg sync.WaitGroup
	errCh := make(chan error, len(m.cfg))

	for name, cfg := range m.cfg {
		if !cfg.Enabled {
			m.servers[name] = &serverState{
				config: cfg,
				status: StateDisabled,
			}
			continue
		}

		wg.Add(1)
		go func(name string, cfg config.MCPServerConfig) {
			defer wg.Done()
			if err := m.startServer(ctx, name, cfg); err != nil {
				if cfg.Required {
					errCh <- fmt.Errorf("required MCP %s: %w", name, err)
				}
			}
		}(name, cfg)
	}

	wg.Wait()
	close(errCh)

	// Check for required failures
	for err := range errCh {
		return err
	}

	return nil
}

func (m *Manager) startServer(ctx context.Context, name string, cfg config.MCPServerConfig) error {
	ss := &serverState{
		config: cfg,
		status: StateStarting,
	}
	m.servers[name] = ss

	timeout := time.Duration(cfg.StartupTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Second
	}

	startCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client, err := NewStdioClient(startCtx, cfg)
	if err != nil {
		ss.status = StateFailed
		ss.initError = err.Error()
		return err
	}

	// Initialize
	initResult, err := client.Initialize(startCtx)
	if err != nil {
		ss.status = StateFailed
		ss.initError = fmt.Sprintf("initialize: %v", err)
		client.Close()
		return err
	}

	// List tools
	tools, err := client.ListTools(startCtx)
	if err != nil {
		ss.status = StateFailed
		ss.initError = fmt.Sprintf("list tools: %v", err)
		client.Close()
		return err
	}

	ss.client = client
	ss.tools = tools
	ss.status = StateRunning

	_ = initResult
	return nil
}

func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, ss := range m.servers {
		if ss.client != nil {
			ss.client.Close()
		}
		ss.status = StateStopped
		_ = name
	}
	return nil
}

func (m *Manager) ListServers() []ServerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []ServerStatus
	for name, ss := range m.servers {
		result = append(result, ServerStatus{
			Name:      name,
			State:     ss.status,
			Tools:     ss.tools,
			InitError: ss.initError,
		})
	}
	return result
}

func (m *Manager) ListTools() []ToolInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	var tools []ToolInfo
	for _, ss := range m.servers {
		if ss.status == StateRunning {
			tools = append(tools, ss.tools...)
		}
	}
	return tools
}

func (m *Manager) CallTool(ctx context.Context, server, tool string, args map[string]interface{}) (ToolResult, error) {
	m.mu.Lock()
	ss, ok := m.servers[server]
	m.mu.Unlock()

	if !ok {
		return ToolResult{}, fmt.Errorf("server %s not found", server)
	}
	if ss.status != StateRunning || ss.client == nil {
		return ToolResult{}, fmt.Errorf("server %s not running", server)
	}

	result, err := ss.client.CallTool(ctx, tool, args)
	if err != nil {
		return ToolResult{}, err
	}

	return result, nil
}

func (m *Manager) GetSchema(qualified string) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ss := range m.servers {
		for _, t := range ss.tools {
			if t.Name == qualified || t.Server+"."+t.Name == qualified {
				return t.Schema, nil
			}
		}
	}
	return nil, fmt.Errorf("tool %s not found", qualified)
}

// NormalizeResult normalizes MCP tool results for model consumption.
func NormalizeResult(result ToolResult) map[string]interface{} {
	// If structured content exists, prefer it
	if len(result.Result) > 0 {
		return result.Result
	}

	// Otherwise use text content
	var textParts []string
	for _, c := range result.Content {
		if c.Type == "text" && c.Text != "" {
			textParts = append(textParts, c.Text)
		}
	}

	if len(textParts) > 0 {
		return map[string]interface{}{"text": textParts[0]}
	}

	return map[string]interface{}{"text": jsonStringify(result)}
}

func jsonStringify(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
