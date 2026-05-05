package builtin

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/kat/katty/internal/config"
)

func registerOS(r *Registry) {
	r.register(&Tool{
		Name:        "katty.os.info",
		Description: "Get OS and environment information",
		Schema:      map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Handler:     osInfo,
	})

	r.register(&Tool{
		Name:        "katty.os.detect",
		Description: "Detect OS distribution and capabilities",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target": map[string]interface{}{"type": "string", "description": "Target name"},
			},
		},
		Handler: osDetect,
	})

	r.register(&Tool{
		Name:        "katty.os.which",
		Description: "Find command paths",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"names": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			},
			"required": []string{"names"},
		},
		Handler: osWhich,
	})

	r.register(&Tool{
		Name:        "katty.os.capabilities",
		Description: "List capability families",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target": map[string]interface{}{"type": "string", "description": "Target name (default: local)"},
			},
		},
		Handler: osCapabilities,
	})
}

func osInfo(ctx context.Context, args map[string]interface{}) ToolResult {
	hostname, _ := os.Hostname()
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	info := map[string]interface{}{
		"current_time": time.Now().Format(time.RFC3339),
		"timezone":     time.Now().Format("MST"),
		"os":           runtime.GOOS,
		"arch":         runtime.GOARCH,
		"username":     os.Getenv("USER"),
		"home":         home,
		"shell":        os.Getenv("SHELL"),
		"cwd":          cwd,
		"hostname":     hostname,
	}

	if out, err := exec.Command("uname", "-a").Output(); err == nil {
		info["uname"] = strings.TrimSpace(string(out))
	}

	// Check for OS release files
	releaseFiles := []string{"/etc/os-release", "/etc/lsb-release", "/etc/debian_version", "/etc/redhat-release"}
	for _, f := range releaseFiles {
		if data, err := os.ReadFile(f); err == nil {
			info[f] = strings.TrimSpace(string(data))
			break
		}
	}

	return okResult(info)
}

func osDetect(ctx context.Context, args map[string]interface{}) ToolResult {
	target := getStr(args, "target")
	if target == "" || target == "local" {
		return osDetectLocal()
	}

	// Remote target - use target.exec
	t, err := getTarget(target)
	if err != nil {
		return errResult("not_found", err.Error())
	}

	detectCmd := "uname -a; cat /etc/os-release 2>/dev/null; cat /etc/*release 2>/dev/null; which sh bash zsh cc gcc clang make git 2>/dev/null"
	res := targetExecSSH(ctx, t, detectCmd, nil, "", "", 15)
	return res
}

func osDetectLocal() ToolResult {
	info := map[string]interface{}{}

	// uname
	if out, err := exec.Command("uname", "-a").Output(); err == nil {
		info["uname"] = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("uname", "-s").Output(); err == nil {
		info["kernel"] = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("uname", "-m").Output(); err == nil {
		info["machine"] = strings.TrimSpace(string(out))
	}

	// OS release
	for _, f := range []string{"/etc/os-release", "/etc/lsb-release"} {
		if data, err := os.ReadFile(f); err == nil {
			info["os_release"] = strings.TrimSpace(string(data))
			break
		}
	}

	// sysctl on macOS
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("sysctl", "-n", "kern.osrelease").Output(); err == nil {
			info["osrelease"] = strings.TrimSpace(string(out))
		}
	}

	// Essential commands
	essentials := []string{"sh", "bash", "cc", "gcc", "clang", "make", "git"}
	found := map[string]string{}
	for _, cmd := range essentials {
		if path, err := exec.LookPath(cmd); err == nil {
			found[cmd] = path
		}
	}
	if len(found) > 0 {
		info["commands"] = found
	}

	return okResult(info)
}

func osWhich(ctx context.Context, args map[string]interface{}) ToolResult {
	var names []string
	if arr, ok := args["names"].([]interface{}); ok {
		for _, n := range arr {
			if s, ok := n.(string); ok {
				names = append(names, s)
			}
		}
	}
	if len(names) == 0 {
		return errResult("invalid_args", "names is required")
	}

	found := map[string]string{}
	missing := []string{}
	for _, name := range names {
		path, err := exec.LookPath(name)
		if err == nil {
			found[name] = path
		} else {
			missing = append(missing, name)
		}
	}

	return okResult(map[string]interface{}{
		"found":   found,
		"missing": missing,
	})
}

// CapabilitiesScan stores the last capability scan result.
var CapabilitiesScan map[string][]string

func osCapabilities(ctx context.Context, args map[string]interface{}) ToolResult {
	if CapabilitiesScan != nil {
		return okResult(map[string]interface{}{
			"target":       getStr(args, "target"),
			"capabilities": CapabilitiesScan,
		})
	}

	// Scan if not done yet
	caps := scanCapabilities(config.CapabilitiesConfig{
		ScanOnStartup: true,
		Families:      defaultFamilies(),
	})
	CapabilitiesScan = caps

	return okResult(map[string]interface{}{
		"target":       "local",
		"capabilities": caps,
	})
}

func defaultFamilies() map[string][]string {
	return map[string][]string{
		"shells":    {"sh", "bash", "zsh", "ksh", "fish"},
		"compilers": {"cc", "gcc", "clang", "go", "rustc"},
		"build":     {"make", "cmake", "ninja", "meson"},
		"debuggers": {"gdb", "lldb"},
		"tracers":   {"strace", "truss", "ktrace", "dtrace", "perf", "bpftrace"},
		"elf_tools": {"readelf", "objdump", "nm", "addr2line"},
		"network":   {"ping", "nc", "curl", "wget", "ssh", "scp", "rsync"},
		"archive":   {"tar", "gzip", "xz", "zip", "unzip"},
		"text":      {"awk", "sed", "grep", "find", "xargs"},
		"docs":      {"man", "apropos", "info"},
		"system":    {"uname", "sysctl", "dmesg", "mount", "ps", "top"},
	}
}

func scanCapabilities(cfg config.CapabilitiesConfig) map[string][]string {
	result := make(map[string][]string)

	for family, tools := range cfg.Families {
		var found []string
		for _, tool := range tools {
			if _, err := exec.LookPath(tool); err == nil {
				found = append(found, tool)
			}
		}
		if len(found) > 0 {
			result[family] = found
		}
	}

	return result
}
