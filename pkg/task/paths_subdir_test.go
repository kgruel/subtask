package task

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProjectDir_WalksUpFromSubdir(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".subtask", "tasks"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".subtask", "internal"), 0o755))
	// config.json is required to identify a project .subtask directory
	require.NoError(t, os.WriteFile(filepath.Join(root, ".subtask", "config.json"), []byte(`{}`), 0o644))

	subdir := filepath.Join(root, "src", "pkg")
	require.NoError(t, os.MkdirAll(subdir, 0o755))

	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(subdir))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	require.Equal(t, filepath.Join("..", "..", ".subtask"), ProjectDir())
	require.Equal(t, filepath.Join("..", "..", ".subtask", "config.json"), ConfigPath())

	expectedRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	require.Equal(t, expectedRoot, ProjectRoot())
	require.Equal(t, filepath.Join(expectedRoot, ".subtask"), ProjectDirAbs())
}

// TestProjectDir_IgnoresGlobalSubtaskDir verifies that a .subtask directory
// without config.json (like the global ~/.subtask for workspaces) is not
// mistaken for a project directory.
func TestProjectDir_IgnoresGlobalSubtaskDir(t *testing.T) {
	// Create a fake "home" with .subtask but NO config.json (like global dir)
	fakeHome := t.TempDir()
	globalSubtask := filepath.Join(fakeHome, ".subtask")
	require.NoError(t, os.MkdirAll(filepath.Join(globalSubtask, "workspaces"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(globalSubtask, "logs"), 0o755))
	// Intentionally NO config.json

	// Create a project directory under fake home
	projectDir := filepath.Join(fakeHome, "code", "myproject")
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(projectDir))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	// Clear the cache since we changed directories
	projectDirCache.mu.Lock()
	projectDirCache.computed = false
	projectDirCache.mu.Unlock()

	// Should NOT find the parent .subtask (no config.json)
	// Should return local .subtask as fallback
	require.Equal(t, ".subtask", ProjectDir())
	require.Equal(t, filepath.Join(".subtask", "config.json"), ConfigPath())

	// For ProjectDirAbs, resolve symlinks on the projectDir part (macOS /var -> /private/var)
	// The .subtask part doesn't exist, so we resolve the parent and append
	resolvedProjectDir, err := filepath.EvalSymlinks(projectDir)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(resolvedProjectDir, ".subtask"), ProjectDirAbs())
}

// TestProjectDir_FindsProjectNotGlobal verifies that when both a project
// .subtask (with config.json) and a global-like .subtask (without config.json)
// exist in the path, only the project one is found.
func TestProjectDir_FindsProjectNotGlobal(t *testing.T) {
	// Create hierarchy: /tmp/home/.subtask (no config) > /tmp/home/code/project/.subtask (with config)
	fakeHome := t.TempDir()

	// Global-like .subtask at "home" level - no config.json
	globalSubtask := filepath.Join(fakeHome, ".subtask")
	require.NoError(t, os.MkdirAll(filepath.Join(globalSubtask, "workspaces"), 0o755))

	// Project .subtask with config.json
	projectRoot := filepath.Join(fakeHome, "code", "project")
	projectSubtask := filepath.Join(projectRoot, ".subtask")
	require.NoError(t, os.MkdirAll(projectSubtask, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(projectSubtask, "config.json"), []byte(`{}`), 0o644))

	// Working in a subdir of the project
	workDir := filepath.Join(projectRoot, "src", "pkg")
	require.NoError(t, os.MkdirAll(workDir, 0o755))

	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	// Clear the cache
	projectDirCache.mu.Lock()
	projectDirCache.computed = false
	projectDirCache.mu.Unlock()

	// Should find project .subtask, not the global-like one
	expectedProjectRoot, err := filepath.EvalSymlinks(projectRoot)
	require.NoError(t, err)
	require.Equal(t, expectedProjectRoot, ProjectRoot())
	require.Equal(t, filepath.Join(expectedProjectRoot, ".subtask"), ProjectDirAbs())
}
