package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type LogType string

const (
	TypeSystem     LogType = "system"
	TypeUser       LogType = "user"
	TypeAssistant  LogType = "assistant"
	TypeToolCall   LogType = "tool_call"
	TypeToolResult LogType = "tool_result"
	TypeToolError  LogType = "tool_error"
)

type Entry struct {
	Timestamp string      `json:"timestamp"`
	Type      LogType     `json:"type"`
	Content   string      `json:"content"`
	Metadata  interface{} `json:"metadata,omitempty"`
}

type Logger struct {
	dir  string
	file *os.File
}

func New(dir string) (*Logger, error) {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".katty", "sessions")
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}

	filename := filepath.Join(dir, fmt.Sprintf("katty-%s.jsonl", time.Now().Format("2006-01-02-150405")))
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open transcript: %w", err)
	}

	return &Logger{dir: dir, file: f}, nil
}

func (l *Logger) Log(logType LogType, content string, metadata interface{}) {
	entry := Entry{
		Timestamp: time.Now().Format(time.RFC3339),
		Type:      logType,
		Content:   content,
		Metadata:  metadata,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}

	l.file.Write(append(data, '\n'))
}

func (l *Logger) Close() {
	if l.file != nil {
		l.file.Close()
	}
}
