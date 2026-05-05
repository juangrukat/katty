package envprobe

import (
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/kat/katty/internal/config"
)

type EnvInfo struct {
	CurrentTime string            `json:"current_time"`
	Timezone    string            `json:"timezone"`
	OS          string            `json:"os"`
	Arch        string            `json:"arch"`
	Username    string            `json:"username"`
	Home        string            `json:"home"`
	Shell       string            `json:"shell"`
	CWD         string            `json:"cwd"`
	Path        string            `json:"path,omitempty"`
	Uname       string            `json:"uname"`
	ToolPaths   map[string]string `json:"tool_paths"`
}

func Probe(cfg config.EnvironmentConfig) EnvInfo {
	info := EnvInfo{
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		ToolPaths: make(map[string]string),
	}

	info.Username = os.Getenv("USER")
	info.Home, _ = os.UserHomeDir()
	info.Shell = os.Getenv("SHELL")
	cwd, _ := os.Getwd()
	info.CWD = cwd

	if cfg.IncludePath {
		info.Path = os.Getenv("PATH")
	}

	// uname
	if out, err := exec.Command("uname", "-a").Output(); err == nil {
		info.Uname = strings.TrimSpace(string(out))
	}

	// tool checks
	if cfg.Enabled {
		for _, tool := range cfg.ToolChecks {
			path, err := exec.LookPath(tool)
			if err == nil {
				info.ToolPaths[tool] = path
			}
		}
	}

	return info
}
