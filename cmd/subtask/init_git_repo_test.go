package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeFakeCLI(t *testing.T, dir string, name string) string {
	t.Helper()

	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, name+".bat")
		require.NoError(t, os.WriteFile(path, []byte("@echo off\r\nexit /B 0\r\n"), 0o644))
		return path
	}

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	return path
}

func TestInit_FailsOutsideGitRepo(t *testing.T) {
	tmpDir := t.TempDir()

	origCwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	// Ensure at least one harness is "available" regardless of the test machine.
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	_ = writeFakeCLI(t, binDir, "codex")

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)

	// Avoid hanging on interactive prompts in the current implementation.
	origStdin := os.Stdin
	devNull, err := os.Open(os.DevNull)
	require.NoError(t, err)
	os.Stdin = devNull
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = devNull.Close()
	})

	_, _, runErr := captureStdoutStderr(t, (&InitCmd{}).Run)
	require.EqualError(t, runErr, "Not a git repository. Run 'git init' first or cd to an existing repo.")
}
