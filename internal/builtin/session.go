package builtin

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Session represents a long-running process session.
type Session struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	PID          int       `json:"pid"`
	Cmd          string    `json:"command"`
	Args         []string  `json:"args"`
	Running      bool      `json:"running"`
	StartedAt    time.Time `json:"started_at"`
	LastOutputAt time.Time `json:"last_output_at"`
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	buffer       *ringBuffer
	cancel       context.CancelFunc
	seq          atomic.Int64
}

type ringBuffer struct {
	mu   sync.Mutex
	data []string
	cap  int
	pos  int
}

func newRingBuffer(cap int) *ringBuffer {
	return &ringBuffer{data: make([]string, cap), cap: cap}
}

func (rb *ringBuffer) Append(line string) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	rb.data[rb.pos] = line
	rb.pos = (rb.pos + 1) % rb.cap
}

func (rb *ringBuffer) Since(seq int64, maxBytes int) ([]string, int64) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	var lines []string
	currentSeq := int64(0)
	totalBytes := 0

	for i := 0; i < rb.cap; i++ {
		idx := (rb.pos + i) % rb.cap
		if rb.data[idx] == "" {
			continue
		}
		if currentSeq >= seq {
			lines = append(lines, rb.data[idx])
			totalBytes += len(rb.data[idx])
			if totalBytes >= maxBytes {
				break
			}
		}
		currentSeq++
	}

	return lines, currentSeq
}

type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

var sessionMgr = &SessionManager{sessions: make(map[string]*Session)}

func registerSession(r *Registry) {
	r.register(&Tool{
		Name:        "katty.session.start",
		Description: "Start a long-running process session",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name":            map[string]interface{}{"type": "string"},
				"cmd":             map[string]interface{}{"type": "string"},
				"args":            map[string]interface{}{"type": "array"},
				"cwd":             map[string]interface{}{"type": "string"},
				"env":             map[string]interface{}{"type": "object"},
				"timeout_seconds": map[string]interface{}{"type": "integer"},
			},
			"required": []string{"cmd"},
		},
		Handler: sessionStart,
	})

	r.register(&Tool{
		Name:        "katty.session.send",
		Description: "Send input to a session",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string"},
				"input":      map[string]interface{}{"type": "string"},
			},
			"required": []string{"session_id", "input"},
		},
		Handler: sessionSend,
	})

	r.register(&Tool{
		Name:        "katty.session.read",
		Description: "Read buffered output from a session",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string"},
				"max_bytes":  map[string]interface{}{"type": "integer"},
				"since_seq":  map[string]interface{}{"type": "integer"},
			},
			"required": []string{"session_id"},
		},
		Handler: sessionRead,
	})

	r.register(&Tool{
		Name:        "katty.session.stop",
		Description: "Stop a session",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{"type": "string"},
				"signal":     map[string]interface{}{"type": "string"},
			},
			"required": []string{"session_id"},
		},
		Handler: sessionStop,
	})

	r.register(&Tool{
		Name:        "katty.session.list",
		Description: "List all sessions",
		Schema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		Handler: sessionList,
	})
}

func sessionStart(ctx context.Context, args map[string]interface{}) ToolResult {
	name := getStr(args, "name")
	cmdPath := getStr(args, "cmd")
	cmdArgs := getStrList(args, "args")
	cwd := getStr(args, "cwd")

	if cmdPath == "" {
		return errResult("invalid_args", "cmd is required")
	}
	if name == "" {
		name = fmt.Sprintf("session-%d", time.Now().Unix())
	}

	sessionMgr.mu.Lock()
	if _, exists := sessionMgr.sessions[name]; exists {
		sessionMgr.mu.Unlock()
		return errResult("exists", fmt.Sprintf("session %s already exists", name))
	}
	sessionMgr.mu.Unlock()

	sessCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(sessCtx, cmdPath, cmdArgs...)

	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Create stdin pipe
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return errResult("io_error", fmt.Sprintf("create stdin pipe: %v", err))
	}

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if cwd != "" {
		cmd.Dir = cwd
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return errResult("io_error", fmt.Sprintf("start session: %v", err))
	}

	buf := newRingBuffer(10000)

	session := &Session{
		ID:        name,
		Name:      name,
		PID:       cmd.Process.Pid,
		Cmd:       cmdPath,
		Args:      cmdArgs,
		Running:   true,
		StartedAt: time.Now(),
		cmd:       cmd,
		stdin:     stdinPipe,
		buffer:    buf,
		cancel:    cancel,
	}

	// Read stdout
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			buf.Append(scanner.Text())
			session.LastOutputAt = time.Now()
			session.seq.Add(1)
		}
	}()

	// Read stderr into same buffer
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			buf.Append("[stderr] " + scanner.Text())
			session.LastOutputAt = time.Now()
			session.seq.Add(1)
		}
	}()

	// Monitor process
	go func() {
		cmd.Wait()
		stdinPipe.Close()
		sessionMgr.mu.Lock()
		session.Running = false
		sessionMgr.mu.Unlock()
	}()

	sessionMgr.mu.Lock()
	sessionMgr.sessions[name] = session
	sessionMgr.mu.Unlock()

	return okResult(map[string]interface{}{
		"session_id": name,
		"pid":        cmd.Process.Pid,
		"cmd":        cmdPath,
		"started":    true,
	})
}

func sessionSend(ctx context.Context, args map[string]interface{}) ToolResult {
	sessionID := getStr(args, "session_id")
	input := getStr(args, "input")

	sessionMgr.mu.Lock()
	session, ok := sessionMgr.sessions[sessionID]
	sessionMgr.mu.Unlock()

	if !ok {
		return errResult("not_found", fmt.Sprintf("session %s not found", sessionID))
	}

	if !session.Running {
		return errResult("stopped", fmt.Sprintf("session %s is not running", sessionID))
	}

	_, err := io.WriteString(session.stdin, input)
	if err != nil {
		return errResult("io_error", fmt.Sprintf("write to session stdin: %v", err))
	}

	return okResult(map[string]interface{}{
		"session_id": sessionID,
		"sent":       true,
	})
}

func sessionRead(ctx context.Context, args map[string]interface{}) ToolResult {
	sessionID := getStr(args, "session_id")
	maxBytes := getInt(args, "max_bytes")
	if maxBytes <= 0 {
		maxBytes = 20000
	}
	sinceSeq := getInt(args, "since_seq")

	sessionMgr.mu.Lock()
	session, ok := sessionMgr.sessions[sessionID]
	sessionMgr.mu.Unlock()

	if !ok {
		return errResult("not_found", fmt.Sprintf("session %s not found", sessionID))
	}

	lines, newSeq := session.buffer.Since(int64(sinceSeq), maxBytes)

	return okResult(map[string]interface{}{
		"session_id": sessionID,
		"output":     lines,
		"seq":        newSeq,
		"running":    session.Running,
	})
}

func sessionStop(ctx context.Context, args map[string]interface{}) ToolResult {
	sessionID := getStr(args, "session_id")
	sigName := getStr(args, "signal")
	if sigName == "" {
		sigName = "TERM"
	}

	sessionMgr.mu.Lock()
	session, ok := sessionMgr.sessions[sessionID]
	sessionMgr.mu.Unlock()

	if !ok {
		return errResult("not_found", fmt.Sprintf("session %s not found", sessionID))
	}

	sig := syscall.SIGTERM
	switch strings.ToUpper(sigName) {
	case "KILL":
		sig = syscall.SIGKILL
	case "INT":
		sig = syscall.SIGINT
	}

	session.cancel()
	if session.cmd.Process != nil {
		syscall.Kill(-session.cmd.Process.Pid, sig)
	}

	sessionMgr.mu.Lock()
	session.Running = false
	sessionMgr.mu.Unlock()

	return okResult(map[string]interface{}{
		"session_id": sessionID,
		"stopped":    true,
	})
}

func sessionList(ctx context.Context, args map[string]interface{}) ToolResult {
	sessionMgr.mu.Lock()
	defer sessionMgr.mu.Unlock()

	var sessions []map[string]interface{}
	for _, s := range sessionMgr.sessions {
		sessions = append(sessions, map[string]interface{}{
			"id":             s.ID,
			"name":           s.Name,
			"pid":            s.PID,
			"command":        s.Cmd,
			"running":        s.Running,
			"started_at":     s.StartedAt.Format(time.RFC3339),
			"last_output_at": s.LastOutputAt.Format(time.RFC3339),
		})
	}

	return okResult(map[string]interface{}{
		"sessions": sessions,
	})
}

// SessionListDirect exposes session list for REPL /sessions command.
func SessionListDirect(ctx context.Context) string {
	sessionMgr.mu.Lock()
	defer sessionMgr.mu.Unlock()

	var lines []string
	for _, s := range sessionMgr.sessions {
		state := "running"
		if !s.Running {
			state = "stopped"
		}
		lines = append(lines, fmt.Sprintf("%s pid=%d cmd=%s state=%s", s.ID, s.PID, s.Cmd, state))
	}
	if len(lines) == 0 {
		return "No active sessions."
	}
	return "Sessions:\n" + fmt.Sprintf("%s", lines)
}
