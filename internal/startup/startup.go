package startup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kat/katty/internal/config"
)

type File struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int    `json:"size"`
}

func Load(cfg config.StartupConfig) ([]File, []string) {
	var files []File
	var warnings []string

	for _, p := range cfg.Files {
		content, err := os.ReadFile(p)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("missing: %s", p))
			continue
		}
		text := string(content)
		if len(text) > cfg.MaxFileChars {
			text = text[:cfg.MaxFileChars]
			warnings = append(warnings, fmt.Sprintf("truncated: %s (%d -> %d chars)", p, len(content), cfg.MaxFileChars))
		}
		files = append(files, File{
			Path:    p,
			Content: text,
			Size:    len(text),
		})
	}

	return files, warnings
}

func EnsureDir() error {
	home, _ := os.UserHomeDir()
	kattyDir := filepath.Join(home, ".katty")

	if err := os.MkdirAll(kattyDir, 0755); err != nil {
		return err
	}

	soulPath := filepath.Join(kattyDir, "soul.md")
	if _, err := os.Stat(soulPath); os.IsNotExist(err) {
		defaultSoul := strings.TrimSpace(`
You are Katty, Kat's local DeepSeek terminal assistant.
You are direct, practical, and grounded.
You are a toolmaking tool for Unix-like systems work.
You help build, inspect, test, debug, and improve tools.
Use Katty tool calls when local or target information is needed.
Do not claim to have run tools unless a tool result is present.
Prefer small composable commands and inspectable steps.
`)
		os.WriteFile(soulPath, []byte(defaultSoul+"\n"), 0644)
	}

	prefsPath := filepath.Join(kattyDir, "preferences.md")
	if _, err := os.Stat(prefsPath); os.IsNotExist(err) {
		defaultPrefs := strings.TrimSpace(`
Kat prefers:
- fast local tooling
- minimal dependencies
- native-feeling command-line tools
- explicit control
- sharp Unix-like capabilities
- distribution-neutral systems work
- tools that help build other tools
`)
		os.WriteFile(prefsPath, []byte(defaultPrefs+"\n"), 0644)
	}

	return nil
}
