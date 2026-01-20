package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogger_PathUsesWorkspaceEscapeConvention(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	l, err := New("/Users/zippo/Code/finality", Options{DebugEnabled: false})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	wantSuffix := filepath.Join(".subtask", "logs", "-Users-zippo-Code-finality.log")
	if !strings.HasSuffix(l.Path(), wantSuffix) {
		t.Fatalf("Path()=%q, want suffix %q", l.Path(), wantSuffix)
	}
}

func TestLogger_DebugIsNoopWhenDisabled(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	l, err := New("/Users/zippo/Code/finality", Options{DebugEnabled: false})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	l.Debug("refresh", "start")
	if _, err := os.Stat(l.Path()); err == nil {
		t.Fatalf("expected no log file created by Debug() when debug disabled, but file exists: %s", l.Path())
	}
}

func TestLogger_RotatesLargeFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	l, err := New("/Users/zippo/Code/finality", Options{DebugEnabled: false, MaxBytes: 10})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	if err := os.MkdirAll(filepath.Dir(l.Path()), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(l.Path(), []byte(strings.Repeat("x", 100)), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	l.Info("tui", "start")

	if _, err := os.Stat(l.Path() + ".1"); err != nil {
		t.Fatalf("expected rotated file %s.1 to exist: %v", l.Path(), err)
	}
	data, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("expected new log file to have content after Info()")
	}
	if !strings.Contains(string(data), "INFO") || !strings.Contains(string(data), "[tui]") {
		t.Fatalf("unexpected log contents: %q", string(data))
	}
}
