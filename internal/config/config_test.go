package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Model.Provider != "deepseek" {
		t.Errorf("expected deepseek provider, got %s", cfg.Model.Provider)
	}
	if cfg.Model.APIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Errorf("expected DEEPSEEK_API_KEY, got %s", cfg.Model.APIKeyEnv)
	}
	if cfg.Tooling.Mode != "auto" {
		t.Errorf("expected auto mode, got %s", cfg.Tooling.Mode)
	}
	if cfg.Tooling.MaxToolRounds != 5 {
		t.Errorf("expected 5 max rounds, got %d", cfg.Tooling.MaxToolRounds)
	}

	// Default target
	if _, ok := cfg.Targets["local"]; !ok {
		t.Error("expected local target in defaults")
	}
	if !cfg.Targets["local"].Default {
		t.Error("local target should be default")
	}

	// Default capabilities
	if !cfg.Capabilities.ScanOnStartup {
		t.Error("expected ScanOnStartup=true")
	}
	if len(cfg.Capabilities.Families) == 0 {
		t.Error("expected capability families in defaults")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadAndExpandPaths(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")

	json := `{"startup": {"max_file_chars": 1000, "files": ["~/test.md"]}, "targets": {"lab": {"type": "ssh", "host": "10.0.0.1", "user": "kat", "identity_file": "~/.ssh/id_ed25519"}}, "mcp_servers": {"test": {"enabled": false, "command": "python3", "cwd": "~/mcp"}}}`
	os.WriteFile(cfgPath, []byte(json), 0644)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	home, _ := os.UserHomeDir()

	// Startup files should have ~ expanded
	if cfg.Startup.Files[0] != filepath.Join(home, "test.md") {
		t.Errorf("startup path not expanded: %s", cfg.Startup.Files[0])
	}

	// Target identity file should be expanded
	if cfg.Targets["lab"].IdentityFile != filepath.Join(home, ".ssh", "id_ed25519") {
		t.Errorf("identity file not expanded: %s", cfg.Targets["lab"].IdentityFile)
	}

	// MCP CWD should be expanded
	if cfg.MCPServers["test"].CWD != filepath.Join(home, "mcp") {
		t.Errorf("mcp cwd not expanded: %s", cfg.MCPServers["test"].CWD)
	}
}

func TestDefaultPath(t *testing.T) {
	path := DefaultPath()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".katty", "config.json")
	if path != expected {
		t.Errorf("expected %s, got %s", expected, path)
	}
}

func TestConfigTranscriptsDir(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Transcripts.Dir != "~/.katty/sessions" {
		t.Errorf("expected ~/.katty/sessions, got %s", cfg.Transcripts.Dir)
	}
}
