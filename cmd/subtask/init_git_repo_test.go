package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zippoxer/subtask/pkg/workspace"
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

// initGitRepo initializes a git repo in dir with an initial commit.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test User")
	readme := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(readme, []byte("# Test\n"), 0o644))
	run("add", ".")
	run("commit", "-m", "Initial commit")
}

// setupInitTest creates an isolated test environment for init tests.
// Returns the temp dir and a cleanup function.
func setupInitTest(t *testing.T, harnesses ...string) string {
	t.Helper()

	tmpDir := t.TempDir()
	initGitRepo(t, tmpDir)

	// Create fake CLI binaries for specified harnesses
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	for _, h := range harnesses {
		writeFakeCLI(t, binDir, h)
	}

	origCwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)

	// Avoid hanging on interactive prompts
	origStdin := os.Stdin
	devNull, err := os.Open(os.DevNull)
	require.NoError(t, err)
	os.Stdin = devNull

	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = devNull.Close()
		_ = os.Chdir(origCwd)
	})

	return tmpDir
}

// TestInit_RespectsHarnessFlag verifies that --harness flag sets the correct harness in config.
func TestInit_RespectsHarnessFlag(t *testing.T) {
	tmpDir := setupInitTest(t, "codex", "claude")

	// Run init with --harness claude
	cmd := &InitCmd{Harness: "claude", Workspaces: 10}
	_, _, err := captureStdoutStderr(t, cmd.Run)
	require.NoError(t, err)

	// Verify config was created with claude harness
	configPath := filepath.Join(tmpDir, ".subtask", "config.json")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var cfg workspace.Config
	require.NoError(t, json.Unmarshal(data, &cfg))
	require.Equal(t, "claude", cfg.Harness)
	require.Equal(t, 10, cfg.MaxWorkspaces)
}

// TestInit_CreatesConfigInCwd verifies that init creates config in cwd,
// not in an ancestor directory that might have .subtask.
func TestInit_CreatesConfigInCwd(t *testing.T) {
	// Create a parent directory with .subtask (simulating an existing project)
	parentDir := t.TempDir()
	initGitRepo(t, parentDir)
	parentSubtask := filepath.Join(parentDir, ".subtask")
	require.NoError(t, os.MkdirAll(parentSubtask, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(parentSubtask, "config.json"),
		[]byte(`{"harness":"codex","max_workspaces":20}`),
		0o644,
	))

	// Create a child directory (new project) - also needs to be a git repo
	childDir := filepath.Join(parentDir, "subproject")
	require.NoError(t, os.MkdirAll(childDir, 0o755))
	initGitRepo(t, childDir)

	// Set up test environment in child dir
	binDir := filepath.Join(childDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	writeFakeCLI(t, binDir, "codex")

	origCwd, _ := os.Getwd()
	require.NoError(t, os.Chdir(childDir))

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)

	origStdin := os.Stdin
	devNull, _ := os.Open(os.DevNull)
	os.Stdin = devNull
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = devNull.Close()
		_ = os.Chdir(origCwd)
	})

	// Run init in child directory
	cmd := &InitCmd{Harness: "codex", Workspaces: 5}
	_, _, err := captureStdoutStderr(t, cmd.Run)
	require.NoError(t, err)

	// Verify config was created in child, not parent
	childConfig := filepath.Join(childDir, ".subtask", "config.json")
	require.FileExists(t, childConfig)

	data, err := os.ReadFile(childConfig)
	require.NoError(t, err)
	var cfg workspace.Config
	require.NoError(t, json.Unmarshal(data, &cfg))
	require.Equal(t, 5, cfg.MaxWorkspaces) // Our value, not parent's 20

	// Parent config should be unchanged
	parentData, err := os.ReadFile(filepath.Join(parentSubtask, "config.json"))
	require.NoError(t, err)
	var parentCfg workspace.Config
	require.NoError(t, json.Unmarshal(parentData, &parentCfg))
	require.Equal(t, 20, parentCfg.MaxWorkspaces) // Still 20
}

// TestInit_RejectsInvalidHarness verifies that --harness with an unsupported
// value is rejected, even if that command exists on PATH.
func TestInit_RejectsInvalidHarness(t *testing.T) {
	// Set up with a fake "invalid" CLI that exists on PATH
	tmpDir := setupInitTest(t, "codex", "invalid-harness")

	// Run init with invalid harness
	cmd := &InitCmd{Harness: "invalid-harness", Workspaces: 10}
	_, _, err := captureStdoutStderr(t, cmd.Run)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid harness")
	require.Contains(t, err.Error(), "Supported harnesses: codex, claude, opencode")

	// Verify no config was created
	configPath := filepath.Join(tmpDir, ".subtask", "config.json")
	require.NoFileExists(t, configPath)
}

// TestInit_DoesNotUseGlobalSubtask verifies that init doesn't detect
// ~/.subtask (global dir without config.json) as an existing project.
func TestInit_DoesNotUseGlobalSubtask(t *testing.T) {
	// Create a fake home with global .subtask (no config.json)
	fakeHome := t.TempDir()
	globalSubtask := filepath.Join(fakeHome, ".subtask")
	require.NoError(t, os.MkdirAll(filepath.Join(globalSubtask, "workspaces"), 0o755))
	// Intentionally NO config.json

	// Create a project directory under fake home
	projectDir := filepath.Join(fakeHome, "code", "myproject")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	initGitRepo(t, projectDir)

	// Set up test environment
	binDir := filepath.Join(projectDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	writeFakeCLI(t, binDir, "codex")

	origCwd, _ := os.Getwd()
	require.NoError(t, os.Chdir(projectDir))

	origPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+origPath)

	origStdin := os.Stdin
	devNull, _ := os.Open(os.DevNull)
	os.Stdin = devNull
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = devNull.Close()
		_ = os.Chdir(origCwd)
	})

	// Run init - should succeed (not see global .subtask as existing project)
	cmd := &InitCmd{Harness: "codex", Workspaces: 10}
	_, _, err := captureStdoutStderr(t, cmd.Run)
	require.NoError(t, err)

	// Verify config was created in project dir
	projectConfig := filepath.Join(projectDir, ".subtask", "config.json")
	require.FileExists(t, projectConfig)

	// Global dir should still not have config.json
	globalConfig := filepath.Join(globalSubtask, "config.json")
	require.NoFileExists(t, globalConfig)
}
