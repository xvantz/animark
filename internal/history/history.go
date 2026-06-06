// Package history provides append-only logging of user actions (status changes,
// episode updates, imports, etc.) in JSONL format.
package history

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry is a single action logged to the history file.
type Entry struct {
	Time    time.Time   `json:"time"`
	UserID  string      `json:"user_id"`
	AnimeID string      `json:"anime_id"`
	Action  string      `json:"action"`  // "set_status", "set_episode", "set_score", "import", "notification"
	OldVal  interface{} `json:"old_val,omitempty"`
	NewVal  interface{} `json:"new_val,omitempty"`
}

// Logger appends JSONL entries to a file. Thread-safe.
type Logger struct {
	mu   sync.Mutex
	path string
	file *os.File
}

// NewLogger opens a JSONL file for appending, creating it if needed.
func NewLogger(path string) (*Logger, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("history mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("history open: %w", err)
	}
	return &Logger{path: path, file: f}, nil
}

// Log appends a single entry to the history file.
func (l *Logger) Log(entry Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry.Time = time.Now().UTC()
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("history marshal: %w", err)
	}
	data = append(data, '\n')

	if _, err := l.file.Write(data); err != nil {
		return fmt.Errorf("history write: %w", err)
	}
	return nil
}

// Close closes the underlying file.
func (l *Logger) Close() error {
	return l.file.Close()
}

// Path returns the file path.
func (l *Logger) Path() string { return l.path }

// Reader reads history entries from a JSONL file.
type Reader struct {
	path string
}

// NewReader creates a reader for history entries.
func NewReader(path string) *Reader {
	return &Reader{path: path}
}

// ReadAll reads all entries from the history file.
func (r *Reader) ReadAll() ([]Entry, error) {
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("history read: %w", err)
	}

	var entries []Entry
	for _, line := range bytesLines(data) {
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // skip malformed lines
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Stats returns aggregated statistics from the history:
// total entries, entries per action type.
func (r *Reader) Stats() (map[string]int, error) {
	entries, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	stats := make(map[string]int)
	for _, e := range entries {
		stats[e.Action]++
	}
	return stats, nil
}

// bytesLines splits a byte slice into lines (without the trailing newline).
func bytesLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
