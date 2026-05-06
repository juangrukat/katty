package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func registerProc(r *Registry) {
	r.register(&Tool{
		Name:        "katty.proc.exec",
		Description: "Execute a command and capture output",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"cmd":             map[string]interface{}{"type": "string", "description": "Command to run (absolute path recommended)"},
				"args":            map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"cwd":             map[string]interface{}{"type": "string", "description": "Working directory"},
				"stdin":           map[string]interface{}{"type": "string", "description": "Optional stdin text"},
				"timeout_seconds": map[string]interface{}{"type": "integer", "description": "Timeout in seconds"},
			},
			"required": []string{"cmd"},
		},
		Handler: procExec,
	})

	r.register(&Tool{
		Name:        "katty.proc.ps",
		Description: "List processes",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filter": map[string]interface{}{"type": "string", "description": "Optional filter text"},
				"all":    map[string]interface{}{"type": "boolean", "description": "Show all processes"},
			},
		},
		Handler: procPs,
	})

	r.register(&Tool{
		Name:        "katty.proc.signal",
		Description: "Send a signal to a process",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pid":    map[string]interface{}{"type": "integer", "description": "Process ID"},
				"signal": map[string]interface{}{"type": "string", "description": "Signal name (e.g., TERM, KILL, INT)"},
			},
			"required": []string{"pid", "signal"},
		},
		Handler: procSignal,
	})
}

func procExec(ctx context.Context, args map[string]interface{}) ToolResult {
	cmdStr := getStr(args, "cmd")
	if cmdStr == "" {
		return errResult("invalid_args", "cmd is required")
	}

	rawArgs := []string{}
	if arr, ok := args["args"].([]interface{}); ok {
		for _, a := range arr {
			if s, ok := a.(string); ok {
				rawArgs = append(rawArgs, s)
			}
		}
	}

	cwd := getStr(args, "cwd")
	stdin := getStr(args, "stdin")
	timeoutSec := getInt(args, "timeout_seconds")
	if timeoutSec <= 0 {
		timeoutSec = 30
	}

	procCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.Command(cmdStr, rawArgs...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	// Prevent child from inheriting terminal stdin by default
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	} else {
		cmd.Stdin = nil
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set process group for clean killing
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return errResult("exec_error", err.Error())
	}
	procMgr.Add(cmd)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		procMgr.Remove(cmd.Process.Pid)
	}()

	var err error
	select {
	case err = <-done:
	case <-procCtx.Done():
		terminateProcessGroup(cmd, done)
		elapsed := time.Since(start)
		kind := "cancelled"
		message := "command cancelled"
		timedOut := false
		if procCtx.Err() == context.DeadlineExceeded {
			kind = "timeout"
			message = fmt.Sprintf("command timed out after %ds", timeoutSec)
			timedOut = true
		}
		return ToolResult{
			IsError: true,
			Error: &ToolError{
				Kind:    kind,
				Message: message,
			},
			Content: map[string]interface{}{
				"cmd":        cmdStr,
				"stdout":     stdout.String(),
				"stderr":     stderr.String(),
				"exit_code":  -1,
				"elapsed_ms": elapsed.Milliseconds(),
				"timed_out":  timedOut,
				"cancelled":  !timedOut,
			},
		}
	}
	elapsed := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return errResult("exec_error", err.Error())
		}
	}

	return okResult(map[string]interface{}{
		"cmd":        cmdStr,
		"args":       rawArgs,
		"stdout":     stdout.String(),
		"stderr":     stderr.String(),
		"exit_code":  exitCode,
		"elapsed_ms": elapsed.Milliseconds(),
	})
}

func terminateProcessGroup(cmd *exec.Cmd, done <-chan error) {
	if cmd.Process != nil {
		signalProcessGroup(cmd, syscall.SIGTERM)
		select {
		case <-done:
			return
		case <-time.After(2 * time.Second):
		}
		signalProcessGroup(cmd, syscall.SIGKILL)
		<-done
	}
}

func killProcessGroup(cmd *exec.Cmd) {
	signalProcessGroup(cmd, syscall.SIGKILL)
}

func signalProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd.Process == nil {
		return
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil {
		syscall.Kill(-pgid, sig)
	}
}

type procInfo struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
}

func procPs(ctx context.Context, args map[string]interface{}) ToolResult {
	filter := getStr(args, "filter")

	cmd := exec.Command("ps", "-eo", "pid,comm")

	output, err := cmd.Output()
	if err != nil {
		return errResult("exec_error", err.Error())
	}

	var processes []procInfo
	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		comm := strings.Join(fields[1:], " ")

		if filter != "" && !strings.Contains(comm, filter) {
			continue
		}

		processes = append(processes, procInfo{
			PID:     pid,
			Command: comm,
		})
	}

	return okResult(map[string]interface{}{
		"processes": processes,
		"count":     len(processes),
	})
}

func procSignal(ctx context.Context, args map[string]interface{}) ToolResult {
	pid := getInt(args, "pid")
	sigName := getStr(args, "signal")
	if sigName == "" {
		sigName = "TERM"
	}

	var sig syscall.Signal
	switch strings.ToUpper(sigName) {
	case "INT":
		sig = syscall.SIGINT
	case "TERM":
		sig = syscall.SIGTERM
	case "KILL":
		sig = syscall.SIGKILL
	case "HUP":
		sig = syscall.SIGHUP
	case "STOP":
		sig = syscall.SIGSTOP
	case "CONT":
		sig = syscall.SIGCONT
	default:
		return errResult("invalid_args", fmt.Sprintf("unknown signal: %s", sigName))
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return errResult("exec_error", fmt.Sprintf("find process %d: %v", pid, err))
	}

	if err := process.Signal(sig); err != nil {
		return errResult("exec_error", fmt.Sprintf("signal %d: %v", pid, err))
	}

	return okResult(map[string]interface{}{
		"pid":    pid,
		"signal": sigName,
		"sent":   true,
	})
}

// -- Process Manager for tracking non-MCP subprocesses --

type ProcManager struct {
	mu    sync.Mutex
	procs map[int]*exec.Cmd
}

var procMgr = &ProcManager{procs: make(map[int]*exec.Cmd)}

func (pm *ProcManager) Add(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.procs[cmd.Process.Pid] = cmd
}

func (pm *ProcManager) Remove(pid int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.procs, pid)
}

func (pm *ProcManager) KillAll() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	for pid, cmd := range pm.procs {
		if cmd.Process != nil {
			signalProcessGroup(cmd, syscall.SIGKILL)
		}
		delete(pm.procs, pid)
	}
}

// ProcPsDirect returns the output of ps aux for REPL /ps command.
func ProcPsDirect(ctx context.Context) string {
	cmd := exec.Command("ps", "aux")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Sprintf("ps error: %v", err)
	}
	return string(out)
}

// ProcKillAll kills all tracked non-MCP subprocesses.
func ProcKillAll() {
	procMgr.KillAll()
}
