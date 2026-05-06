package startup

import (
	"fmt"
	"os"
	"path/filepath"

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

func EnsureDir(defaultSoul, defaultPreferences string) error {
	home, _ := os.UserHomeDir()
	kattyDir := filepath.Join(home, ".katty")

	if err := os.MkdirAll(kattyDir, 0755); err != nil {
		return err
	}

	soulPath := filepath.Join(kattyDir, "soul.md")
	if _, err := os.Stat(soulPath); os.IsNotExist(err) {
		os.WriteFile(soulPath, []byte(ensureTrailingNewline(defaultSoul)), 0644)
	}

	prefsPath := filepath.Join(kattyDir, "preferences.md")
	if _, err := os.Stat(prefsPath); os.IsNotExist(err) {
		os.WriteFile(prefsPath, []byte(ensureTrailingNewline(defaultPreferences)), 0644)
	}

	return nil
}

func ensureTrailingNewline(s string) string {
	if s == "" || s[len(s)-1] == '\n' {
		return s
	}
	return s + "\n"
}
