package builtin

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func registerNet(r *Registry) {
	r.register(&Tool{
		Name:        "katty.net.check",
		Description: "Check network reachability of a host and ports",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"host":            map[string]interface{}{"type": "string", "description": "Hostname or IP"},
				"ports":           map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "integer"}},
				"timeout_seconds": map[string]interface{}{"type": "integer", "description": "Timeout per check in seconds"},
			},
			"required": []string{"host"},
		},
		Handler: netCheck,
	})
}

type portStatus struct {
	Port  int    `json:"port"`
	Open  bool   `json:"open"`
	Error string `json:"error,omitempty"`
}

func netCheck(ctx context.Context, args map[string]interface{}) ToolResult {
	host := getStr(args, "host")
	if host == "" {
		return errResult("invalid_args", "host is required")
	}

	timeoutSec := getInt(args, "timeout_seconds")
	if timeoutSec <= 0 {
		timeoutSec = 2
	}
	timeout := time.Duration(timeoutSec) * time.Second

	var ports []int
	if arr, ok := args["ports"].([]interface{}); ok {
		for _, p := range arr {
			switch v := p.(type) {
			case float64:
				ports = append(ports, int(v))
			case int:
				ports = append(ports, v)
			}
		}
	}

	result := map[string]interface{}{
		"host": host,
	}

	if len(ports) == 0 {
		// Just do a basic reachability check with net.DialTimeout
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, "22"), timeout)
		if err != nil {
			result["reachable"] = false
			result["error"] = err.Error()
		} else {
			conn.Close()
			result["reachable"] = true
		}
	} else {
		results := make([]portStatus, len(ports))
		for i, port := range ports {
			addr := net.JoinHostPort(host, strconv.Itoa(port))
			conn, err := net.DialTimeout("tcp", addr, timeout)
			if err != nil {
				results[i] = portStatus{Port: port, Open: false, Error: err.Error()}
			} else {
				conn.Close()
				results[i] = portStatus{Port: port, Open: true}
			}
		}
		result["ports"] = results
	}

	// Also try ping for basic ICMP reachability
	if pingResult := tryPing(host, timeoutSec); pingResult != "" {
		result["ping"] = pingResult
	}

	return okResult(result)
}

func tryPing(host string, timeoutSec int) string {
	// Try ping with count 1 and timeout
	countFlag := "-c"
	timeoutFlag := "-W"
	if _, err := exec.LookPath("ping"); err != nil {
		return ""
	}

	cmd := exec.Command("ping", countFlag, "1", timeoutFlag, strconv.Itoa(timeoutSec), host)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Sprintf("ping failed: %v", err)
	}

	// Extract useful info
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if strings.Contains(line, "bytes from") || strings.Contains(line, "1 packets received") || strings.Contains(line, "100.0% packet loss") {
			return strings.TrimSpace(line)
		}
	}
	return strings.TrimSpace(string(out))
}
