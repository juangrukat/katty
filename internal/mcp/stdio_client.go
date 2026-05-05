package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/kat/katty/internal/config"
)

// StdioClient manages a single MCP server process over stdio.
type StdioClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	stderr *bufio.Scanner

	mu      sync.Mutex
	reqID   atomic.Int64
	pending map[int64]chan jsonrpcResponse
	closeCh chan struct{}
}

type jsonrpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func NewStdioClient(ctx context.Context, cfg config.MCPServerConfig) (*StdioClient, error) {
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
	if cfg.CWD != "" {
		cmd.Dir = cfg.CWD
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start: %w", err)
	}

	c := &StdioClient{
		cmd:     cmd,
		stdin:   stdin,
		stdout:  bufio.NewScanner(stdoutPipe),
		stderr:  bufio.NewScanner(stderrPipe),
		pending: make(map[int64]chan jsonrpcResponse),
		closeCh: make(chan struct{}),
	}

	// Start stdout reader goroutine
	go c.readStdout()

	// Stderr reader (just discard for now)
	go func() {
		for c.stderr.Scan() {
			// Store recent stderr lines if needed
		}
	}()

	return c, nil
}

func (c *StdioClient) readStdout() {
	for {
		select {
		case <-c.closeCh:
			return
		default:
		}

		if !c.stdout.Scan() {
			return
		}

		line := c.stdout.Text()
		var resp jsonrpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}

		c.mu.Lock()
		ch, ok := c.pending[resp.ID]
		c.mu.Unlock()

		if ok {
			ch <- resp
		}
	}
}

func (c *StdioClient) call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	id := c.reqID.Add(1)

	req := jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	ch := make(chan jsonrpcResponse, 1)

	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	if _, err := io.WriteString(c.stdin, string(data)+"\n"); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *StdioClient) Initialize(ctx context.Context) (map[string]interface{}, error) {
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo": map[string]interface{}{
			"name":    "katty",
			"version": "1.0.0",
		},
	}

	result, err := c.call(ctx, "initialize", params)
	if err != nil {
		return nil, err
	}

	var info map[string]interface{}
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, err
	}

	// Send initialized notification
	notify := jsonrpcRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}
	data, _ := json.Marshal(notify)
	io.WriteString(c.stdin, string(data)+"\n")

	return info, nil
}

func (c *StdioClient) ListTools(ctx context.Context) ([]ToolInfo, error) {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Tools []struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			InputSchema map[string]interface{} `json:"inputSchema"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, err
	}

	var tools []ToolInfo
	for _, t := range resp.Tools {
		tools = append(tools, ToolInfo{
			Name:        t.Name,
			Description: t.Description,
			Schema:      t.InputSchema,
		})
	}

	return tools, nil
}

func (c *StdioClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (ToolResult, error) {
	params := map[string]interface{}{
		"name":      name,
		"arguments": args,
	}

	result, err := c.call(ctx, "tools/call", params)
	if err != nil {
		return ToolResult{}, err
	}

	var tr ToolResult
	if err := json.Unmarshal(result, &tr); err != nil {
		return ToolResult{}, err
	}

	return tr, nil
}

func (c *StdioClient) Close() error {
	close(c.closeCh)
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	return nil
}
