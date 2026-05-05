package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Model        ModelConfig                `json:"model"`
	Startup      StartupConfig              `json:"startup"`
	Environment  EnvironmentConfig          `json:"environment"`
	Capabilities CapabilitiesConfig         `json:"capabilities"`
	Targets      map[string]Target          `json:"targets"`
	MCPServers   map[string]MCPServerConfig `json:"mcp_servers"`
	Tooling      ToolingConfig              `json:"tooling"`
	Output       OutputConfig               `json:"output"`
	MCP          MCPConfig                  `json:"mcp"`
	Transcripts  TranscriptsConfig          `json:"transcripts"`
}

type ModelConfig struct {
	Provider              string `json:"provider"`
	Model                 string `json:"model"`
	APIKeyEnv             string `json:"api_key_env"`
	BaseURL               string `json:"base_url"`
	RequestTimeoutSeconds int    `json:"request_timeout_seconds"`
}

type StartupConfig struct {
	MaxFileChars int      `json:"max_file_chars"`
	Files        []string `json:"files"`
}

type EnvironmentConfig struct {
	Enabled     bool     `json:"enabled"`
	IncludePath bool     `json:"include_path"`
	ToolChecks  []string `json:"tool_checks"`
}

type CapabilitiesConfig struct {
	ScanOnStartup bool                `json:"scan_on_startup"`
	Families      map[string][]string `json:"families"`
}

type Target struct {
	Type                  string `json:"type"`
	Default               bool   `json:"default,omitempty"`
	Host                  string `json:"host,omitempty"`
	User                  string `json:"user,omitempty"`
	Port                  int    `json:"port,omitempty"`
	IdentityFile          string `json:"identity_file,omitempty"`
	ConnectTimeoutSeconds int    `json:"connect_timeout_seconds,omitempty"`
}

type MCPServerConfig struct {
	Enabled               bool     `json:"enabled"`
	Required              bool     `json:"required"`
	Transport             string   `json:"transport"`
	Command               string   `json:"command"`
	Args                  []string `json:"args"`
	CWD                   string   `json:"cwd"`
	StartupTimeoutSeconds int      `json:"startup_timeout_seconds"`
	CallTimeoutSeconds    int      `json:"call_timeout_seconds"`
}

type ToolingConfig struct {
	Mode          string `json:"mode"`
	MaxToolRounds int    `json:"max_tool_rounds"`
}

type OutputConfig struct {
	MaxToolOutputChars      int  `json:"max_tool_output_chars"`
	PreferStructuredContent bool `json:"prefer_structured_content"`
	IncludeRawMCPEnvelope   bool `json:"include_raw_mcp_envelope"`
	CompactShellResults     bool `json:"compact_shell_results"`
}

type MCPConfig struct {
	CacheTools         bool `json:"cache_tools"`
	CacheTTLSeconds    int  `json:"cache_ttl_seconds"`
	StartupConcurrency bool `json:"startup_concurrency"`
}

type TranscriptsConfig struct {
	Dir    string `json:"dir"`
	Format string `json:"format"`
}

// TargetConfigRegistry holds the runtime target state.
type TargetConfigRegistry struct {
	Targets map[string]Target
}

func DefaultConfig() Config {
	home, _ := os.UserHomeDir()
	return Config{
		Model: ModelConfig{
			Provider:              "deepseek",
			Model:                 "deepseek-chat",
			APIKeyEnv:             "DEEPSEEK_API_KEY",
			BaseURL:               "https://api.deepseek.com",
			RequestTimeoutSeconds: 120,
		},
		Startup: StartupConfig{
			MaxFileChars: 20000,
			Files: []string{
				filepath.Join(home, ".katty", "soul.md"),
				filepath.Join(home, ".katty", "preferences.md"),
			},
		},
		Environment: EnvironmentConfig{
			Enabled:     true,
			IncludePath: true,
			ToolChecks: []string{
				"sh", "bash", "zsh", "git", "cc", "gcc", "clang",
				"make", "cmake", "ninja", "go", "python3", "perl",
				"awk", "sed", "grep", "find", "xargs", "tar", "gzip",
				"curl", "wget", "ssh", "scp", "rsync", "nc", "ping",
				"man", "gdb", "lldb", "strace", "truss", "ktrace",
				"dtrace", "perf", "bpftrace", "readelf", "objdump",
				"nm", "addr2line", "hexdump", "xxd",
			},
		},
		Capabilities: CapabilitiesConfig{
			ScanOnStartup: true,
			Families: map[string][]string{
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
			},
		},
		Targets: map[string]Target{
			"local": {
				Type:    "local",
				Default: true,
			},
		},
		MCPServers: map[string]MCPServerConfig{},
		Tooling: ToolingConfig{
			Mode:          "auto",
			MaxToolRounds: 5,
		},
		Output: OutputConfig{
			MaxToolOutputChars:      20000,
			PreferStructuredContent: true,
			IncludeRawMCPEnvelope:   false,
			CompactShellResults:     true,
		},
		MCP: MCPConfig{
			CacheTools:         true,
			CacheTTLSeconds:    3600,
			StartupConcurrency: true,
		},
		Transcripts: TranscriptsConfig{
			Dir:    filepath.Join(home, ".katty", "sessions"),
			Format: "jsonl",
		},
	}
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.expandPaths()
	return &cfg, nil
}

func (c *Config) expandPaths() {
	home, _ := os.UserHomeDir()
	expand := func(p string) string {
		if strings.HasPrefix(p, "~/") {
			return filepath.Join(home, p[2:])
		}
		return p
	}
	for i, f := range c.Startup.Files {
		c.Startup.Files[i] = expand(f)
	}
	c.Transcripts.Dir = expand(c.Transcripts.Dir)
	for name, t := range c.Targets {
		if t.IdentityFile != "" {
			t.IdentityFile = expand(t.IdentityFile)
			c.Targets[name] = t
		}
	}
	for name, m := range c.MCPServers {
		m.CWD = expand(m.CWD)
		c.MCPServers[name] = m
	}
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".katty", "config.json")
}
