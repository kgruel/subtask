package logging

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLogger_PathUsesWorkspaceEscapeConvention(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	const root = "/Users/zippo/Code/finality"

	l, err := New(root, Options{DebugEnabled: false})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// The escaped name is platform-specific, and deliberately so. On Windows a
	// bare "/Users/..." is drive-relative, so it absolutizes against the current
	// drive and the drive letter lands in the escaped name ("D--Users-zippo-..."
	// rather than "-Users-zippo-..."). That is the correct convention, not a
	// quirk to normalize away: D:\repo and C:\repo are different roots, and
	// dropping the letter would collide their workspaces and project state.
	name := "-Users-zippo-Code-finality"
	if runtime.GOOS == "windows" {
		abs, err := filepath.Abs(root)
		if err != nil {
			t.Fatalf("Abs(%q): %v", root, err)
		}
		// e.g. "D:" -> "D-", matching the ':' replacement in the convention.
		name = strings.ReplaceAll(filepath.VolumeName(abs), ":", "-") + name
	}

	wantSuffix := filepath.Join(".subtask", "logs", name+".log")
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
