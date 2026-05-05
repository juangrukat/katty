package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/kat/katty/internal/config"
)

// TargetRegistry is populated at startup with configured targets.
var TargetRegistry *config.TargetConfigRegistry

type TargetInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Default bool   `json:"default"`
	Host    string `json:"host,omitempty"`
	User    string `json:"user,omitempty"`
	Port    int    `json:"port,omitempty"`
	Online  bool   `json:"online,omitempty"`
}

func registerTarget(r *Registry) {
	r.register(&Tool{
		Name:        "katty.target.list",
		Description: "List configured targets",
		Schema:      map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Handler:     targetList,
	})

	r.register(&Tool{
		Name:        "katty.target.info",
		Description: "Get target information",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target": map[string]interface{}{"type": "string", "description": "Target name"},
			},
			"required": []string{"target"},
		},
		Handler: targetInfo,
	})

	r.register(&Tool{
		Name:        "katty.target.ping",
		Description: "Check target reachability",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target": map[string]interface{}{"type": "string", "description": "Target name"},
			},
			"required": []string{"target"},
		},
		Handler: targetPing,
	})

	r.register(&Tool{
		Name:        "katty.target.exec",
		Description: "Execute command on target",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target":          map[string]interface{}{"type": "string", "description": "Target name"},
				"cmd":             map[string]interface{}{"type": "string", "description": "Command to run"},
				"args":            map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
				"cwd":             map[string]interface{}{"type": "string", "description": "Working directory"},
				"stdin":           map[string]interface{}{"type": "string", "description": "Optional stdin"},
				"timeout_seconds": map[string]interface{}{"type": "integer", "description": "Timeout in seconds"},
			},
			"required": []string{"target", "cmd"},
		},
		Handler: targetExec,
	})

	r.register(&Tool{
		Name:        "katty.target.copy_to",
		Description: "Copy file to target",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target":    map[string]interface{}{"type": "string", "description": "Target name"},
				"src":       map[string]interface{}{"type": "string", "description": "Local source path"},
				"dst":       map[string]interface{}{"type": "string", "description": "Remote destination path"},
				"recursive": map[string]interface{}{"type": "boolean", "description": "Copy recursively"},
			},
			"required": []string{"target", "src", "dst"},
		},
		Handler: targetCopyTo,
	})

	r.register(&Tool{
		Name:        "katty.target.copy_from",
		Description: "Copy file from target",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target":    map[string]interface{}{"type": "string", "description": "Target name"},
				"src":       map[string]interface{}{"type": "string", "description": "Remote source path"},
				"dst":       map[string]interface{}{"type": "string", "description": "Local destination path"},
				"recursive": map[string]interface{}{"type": "boolean", "description": "Copy recursively"},
			},
			"required": []string{"target", "src", "dst"},
		},
		Handler: targetCopyFrom,
	})
}

func getTarget(name string) (config.Target, error) {
	if TargetRegistry == nil {
		return config.Target{}, fmt.Errorf("no targets configured")
	}
	t, ok := TargetRegistry.Targets[name]
	if !ok {
		return config.Target{}, fmt.Errorf("target '%s' not found", name)
	}
	return t, nil
}

func targetList(ctx context.Context, args map[string]interface{}) ToolResult {
	if TargetRegistry == nil {
		return okResult(map[string]interface{}{"targets": []TargetInfo{}, "count": 0})
	}

	var targets []TargetInfo
	for name, t := range TargetRegistry.Targets {
		info := TargetInfo{
			Name:    name,
			Type:    t.Type,
			Default: t.Default,
			Host:    t.Host,
			User:    t.User,
			Port:    t.Port,
		}
		targets = append(targets, info)
	}

	return okResult(map[string]interface{}{
		"targets": targets,
		"count":   len(targets),
	})
}

func targetInfo(ctx context.Context, args map[string]interface{}) ToolResult {
	name := getStr(args, "target")
	t, err := getTarget(name)
	if err != nil {
		return errResult("not_found", err.Error())
	}

	if t.Type == "local" {
		return targetInfoLocal()
	}

	return targetInfoSSH(ctx, t)
}

func targetInfoLocal() ToolResult {
	hostname, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	info := map[string]interface{}{
		"target":   "local",
		"type":     "local",
		"hostname": hostname,
		"user":     os.Getenv("USER"),
		"home":     home,
		"os":       os.Getenv("OSTYPE"),
	}

	// uname
	if out, err := exec.Command("uname", "-a").Output(); err == nil {
		info["uname"] = strings.TrimSpace(string(out))
	}

	return okResult(info)
}

func targetInfoSSH(ctx context.Context, t config.Target) ToolResult {
	target := fmt.Sprintf("%s@%s", t.User, t.Host)
	args := sshArgs(t, "uname -a; cat /etc/os-release 2>/dev/null || cat /etc/*release 2>/dev/null || echo no-release")
	cmd := exec.CommandContext(ctx, "ssh", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	info := map[string]interface{}{
		"target": target,
		"type":   "ssh",
		"host":   t.Host,
		"user":   t.User,
		"port":   t.Port,
	}

	if err != nil {
		info["error"] = err.Error()
		info["stderr"] = stderr.String()
	} else {
		info["output"] = stdout.String()
	}

	return okResult(info)
}

func targetPing(ctx context.Context, args map[string]interface{}) ToolResult {
	name := getStr(args, "target")
	t, err := getTarget(name)
	if err != nil {
		return errResult("not_found", err.Error())
	}

	if t.Type == "local" {
		return okResult(map[string]interface{}{
			"target": "local",
			"online": true,
		})
	}

	// Use nc to check SSH port
	timeout := 2 * time.Second
	if t.ConnectTimeoutSeconds > 0 {
		timeout = time.Duration(t.ConnectTimeoutSeconds) * time.Second
	}

	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(pingCtx, "nc", "-z", "-w", fmt.Sprintf("%d", int(timeout.Seconds())), t.Host, fmt.Sprintf("%d", t.Port))
	cmd.Stdin = nil
	err = cmd.Run()

	online := err == nil
	info := map[string]interface{}{
		"target": name,
		"host":   t.Host,
		"port":   t.Port,
		"online": online,
	}
	if !online {
		info["error"] = err.Error()
	}

	return okResult(info)
}

func targetExec(ctx context.Context, args map[string]interface{}) ToolResult {
	name := getStr(args, "target")
	cmdStr := getStr(args, "cmd")
	t, err := getTarget(name)
	if err != nil {
		return errResult("not_found", err.Error())
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

	if t.Type == "local" {
		// Route to proc.exec behavior
		return procExecLocal(ctx, cmdStr, rawArgs, cwd, stdin, timeoutSec)
	}

	return targetExecSSH(ctx, t, cmdStr, rawArgs, cwd, stdin, timeoutSec)
}

func procExecLocal(ctx context.Context, cmdStr string, rawArgs []string, cwd, stdin string, timeoutSec int) ToolResult {
	procCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(procCtx, cmdStr, rawArgs...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	} else {
		cmd.Stdin = nil
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if procCtx.Err() != nil {
			killProcessGroup(cmd)
			return ToolResult{
				IsError: true,
				Error:   &ToolError{Kind: "timeout", Message: fmt.Sprintf("command timed out after %ds", timeoutSec)},
				Content: map[string]interface{}{
					"cmd": cmdStr, "stdout": stdout.String(), "stderr": stderr.String(),
					"exit_code": -1, "elapsed_ms": elapsed.Milliseconds(), "timed_out": true,
				},
			}
		} else {
			return errResult("exec_error", err.Error())
		}
	}

	return okResult(map[string]interface{}{
		"target":     "local",
		"cmd":        cmdStr,
		"args":       rawArgs,
		"stdout":     stdout.String(),
		"stderr":     stderr.String(),
		"exit_code":  exitCode,
		"elapsed_ms": elapsed.Milliseconds(),
	})
}

func targetExecSSH(ctx context.Context, t config.Target, cmdStr string, rawArgs []string, cwd, stdin string, timeoutSec int) ToolResult {
	procCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// Build remote command
	remoteCmd := cmdStr
	if len(rawArgs) > 0 {
		remoteCmd += " " + strings.Join(quoteArgs(rawArgs), " ")
	}
	if cwd != "" {
		remoteCmd = fmt.Sprintf("cd %s && %s", shellQuote(cwd), remoteCmd)
	}
	if stdin != "" {
		escaped := strings.ReplaceAll(stdin, "'", `'\''`)
		remoteCmd = fmt.Sprintf("echo '%s' | %s", escaped, remoteCmd)
	}

	sshArgs := sshArgs(t, remoteCmd)
	cmd := exec.CommandContext(procCtx, "ssh", sshArgs...)
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if procCtx.Err() != nil {
			killProcessGroup(cmd)
			return ToolResult{
				IsError: true,
				Error:   &ToolError{Kind: "timeout", Message: fmt.Sprintf("ssh command timed out after %ds", timeoutSec)},
				Content: map[string]interface{}{
					"target": fmt.Sprintf("%s@%s", t.User, t.Host), "cmd": cmdStr,
					"stdout": stdout.String(), "stderr": stderr.String(),
					"exit_code": -1, "elapsed_ms": elapsed.Milliseconds(), "timed_out": true,
				},
			}
		} else {
			return errResult("exec_error", fmt.Sprintf("ssh: %v (stderr: %s)", err, stderr.String()))
		}
	}

	return okResult(map[string]interface{}{
		"target":     fmt.Sprintf("%s@%s", t.User, t.Host),
		"cmd":        cmdStr,
		"args":       rawArgs,
		"stdout":     stdout.String(),
		"stderr":     stderr.String(),
		"exit_code":  exitCode,
		"elapsed_ms": elapsed.Milliseconds(),
	})
}

func targetCopyTo(ctx context.Context, args map[string]interface{}) ToolResult {
	name := getStr(args, "target")
	src := getStr(args, "src")
	dst := getStr(args, "dst")
	recursive := getBool(args, "recursive")

	t, err := getTarget(name)
	if err != nil {
		return errResult("not_found", err.Error())
	}

	if t.Type == "local" {
		res := fsCopy(ctx, map[string]interface{}{
			"src": src, "dst": dst, "recursive": recursive, "overwrite": true,
		})
		return res
	}

	// Use scp
	scpArgs := []string{}
	if recursive {
		scpArgs = append(scpArgs, "-r")
	}
	if t.Port != 22 {
		scpArgs = append(scpArgs, "-P", fmt.Sprintf("%d", t.Port))
	}
	if t.IdentityFile != "" {
		scpArgs = append(scpArgs, "-i", t.IdentityFile)
	}
	if t.ConnectTimeoutSeconds > 0 {
		scpArgs = append(scpArgs, "-o", fmt.Sprintf("ConnectTimeout=%d", t.ConnectTimeoutSeconds))
	}
	scpArgs = append(scpArgs, "-o", "StrictHostKeyChecking=accept-new")
	scpArgs = append(scpArgs, "-o", "BatchMode=yes")
	scpArgs = append(scpArgs, src)
	scpArgs = append(scpArgs, fmt.Sprintf("%s@%s:%s", t.User, t.Host, dst))

	cmd := exec.CommandContext(ctx, "scp", scpArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return errResult("exec_error", fmt.Sprintf("scp: %v (stderr: %s)", err, stderr.String()))
	}

	return okResult(map[string]interface{}{
		"target": name,
		"src":    src,
		"dst":    dst,
		"copied": true,
	})
}

func targetCopyFrom(ctx context.Context, args map[string]interface{}) ToolResult {
	name := getStr(args, "target")
	src := getStr(args, "src")
	dst := getStr(args, "dst")
	recursive := getBool(args, "recursive")

	t, err := getTarget(name)
	if err != nil {
		return errResult("not_found", err.Error())
	}

	if t.Type == "local" {
		res := fsCopy(ctx, map[string]interface{}{
			"src": src, "dst": dst, "recursive": recursive, "overwrite": true,
		})
		return res
	}

	// Use scp (reverse direction)
	scpArgs := []string{}
	if recursive {
		scpArgs = append(scpArgs, "-r")
	}
	if t.Port != 22 {
		scpArgs = append(scpArgs, "-P", fmt.Sprintf("%d", t.Port))
	}
	if t.IdentityFile != "" {
		scpArgs = append(scpArgs, "-i", t.IdentityFile)
	}
	if t.ConnectTimeoutSeconds > 0 {
		scpArgs = append(scpArgs, "-o", fmt.Sprintf("ConnectTimeout=%d", t.ConnectTimeoutSeconds))
	}
	scpArgs = append(scpArgs, "-o", "StrictHostKeyChecking=accept-new")
	scpArgs = append(scpArgs, "-o", "BatchMode=yes")
	scpArgs = append(scpArgs, fmt.Sprintf("%s@%s:%s", t.User, t.Host, src))
	scpArgs = append(scpArgs, dst)

	cmd := exec.CommandContext(ctx, "scp", scpArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return errResult("exec_error", fmt.Sprintf("scp: %v (stderr: %s)", err, stderr.String()))
	}

	return okResult(map[string]interface{}{
		"target": name,
		"src":    src,
		"dst":    dst,
		"copied": true,
	})
}

func sshArgs(t config.Target, remoteCmd string) []string {
	args := []string{}
	if t.Port != 0 && t.Port != 22 {
		args = append(args, "-p", fmt.Sprintf("%d", t.Port))
	}
	if t.IdentityFile != "" {
		args = append(args, "-i", t.IdentityFile)
	}
	if t.ConnectTimeoutSeconds > 0 {
		args = append(args, "-o", fmt.Sprintf("ConnectTimeout=%d", t.ConnectTimeoutSeconds))
	}
	args = append(args, "-o", "StrictHostKeyChecking=accept-new")
	args = append(args, "-o", "BatchMode=yes")
	args = append(args, fmt.Sprintf("%s@%s", t.User, t.Host))
	args = append(args, remoteCmd)
	return args
}

func quoteArgs(args []string) []string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = shellQuote(a)
	}
	return quoted
}

func shellQuote(s string) string {
	if strings.ContainsAny(s, " \t\n\r'\"$`\\|&;()<>{}[]*?!~") {
		return fmt.Sprintf("'%s'", strings.ReplaceAll(s, "'", `'\''`))
	}
	return s
}

// SetTargetRegistry stores config targets in the registry used by target tools.
func SetTargetRegistry(cfg map[string]config.Target) {
	if TargetRegistry == nil {
		TargetRegistry = &config.TargetConfigRegistry{}
	}
	TargetRegistry.Targets = cfg
}
