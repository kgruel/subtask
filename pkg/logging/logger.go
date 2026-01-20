package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Level string

const (
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
	LevelError Level = "ERROR"
)

type Options struct {
	// DebugEnabled controls whether DEBUG logs are written.
	// When using Default(), this is derived from SUBTASK_DEBUG once at init.
	DebugEnabled bool

	// MaxBytes controls log rotation. If zero, a default is used.
	MaxBytes int64

	Now func() time.Time
}

type Logger struct {
	debugEnabled bool
	path         string
	maxBytes     int64
	now          func() time.Time

	mu   sync.Mutex
	file *os.File
}

const defaultMaxBytes = 8 << 20 // 8 MiB

func New(projectRoot string, opts Options) (*Logger, error) {
	if strings.TrimSpace(projectRoot) == "" {
		return nil, fmt.Errorf("project root is required")
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}

	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}

	escaped := escapePath(projectRoot)
	logDir := filepath.Join(globalDir(), "logs")
	path := filepath.Join(logDir, escaped+".log")

	return &Logger{
		debugEnabled: opts.DebugEnabled,
		path:         path,
		maxBytes:     maxBytes,
		now:          now,
	}, nil
}

func (l *Logger) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *Logger) DebugEnabled() bool {
	if l == nil {
		return false
	}
	return l.debugEnabled
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *Logger) Debug(op, msg string) {
	if l == nil || !l.debugEnabled {
		return
	}
	l.write(LevelDebug, op, msg)
}

func (l *Logger) Info(op, msg string) {
	if l == nil {
		return
	}
	l.write(LevelInfo, op, msg)
}

func (l *Logger) Error(op, msg string) {
	if l == nil {
		return
	}
	l.write(LevelError, op, msg)
}

// DebugTimer logs a DEBUG start message and returns a closure that logs a DEBUG done message with duration.
// When DEBUG is disabled, it returns a no-op closure and does not call time.Now().
func (l *Logger) DebugTimer(op, startMsg string) func(doneMsg string) {
	if l == nil || !l.debugEnabled {
		return func(string) {}
	}
	start := time.Now()
	l.Debug(op, startMsg)
	return func(doneMsg string) {
		d := time.Since(start)
		l.Debug(op, fmt.Sprintf("%s (%s)", doneMsg, formatDurationMS(d)))
	}
}

func (l *Logger) write(level Level, op, msg string) {
	ts := l.now().UTC().Format("2006-01-02T15:04:05.000Z")
	op = strings.TrimSpace(op)
	if op == "" {
		op = "log"
	}

	line := fmt.Sprintf("%s %-5s [%s] %s\n", ts, level, op, strings.TrimSpace(msg))

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		if err := l.openLocked(); err != nil {
			return
		}
	}

	_, _ = l.file.WriteString(line)
}

func (l *Logger) openLocked() error {
	if l.file != nil {
		return nil
	}
	if l.path == "" {
		return fmt.Errorf("log file path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}

	if err := rotateIfNeeded(l.path, l.maxBytes); err != nil {
		// Best-effort rotation: continue even if rotation fails.
	}

	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	l.file = f
	return nil
}

func rotateIfNeeded(path string, maxBytes int64) error {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if st.Size() <= maxBytes {
		return nil
	}

	backup := path + ".1"
	_ = os.Remove(backup)
	if err := os.Rename(path, backup); err == nil {
		return nil
	}

	// Fallback: truncate in place.
	return os.WriteFile(path, nil, 0o644)
}

func formatDurationMS(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d < time.Second {
		return d.Round(time.Millisecond).String()
	}
	return d.Round(10 * time.Millisecond).String()
}
