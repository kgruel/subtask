package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/internal/binaryupdate"
)

func TestApplyStagedAndSyncPlugin_Success_InvokesSyncWithNewBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "subtask")
	require.NoError(t, os.WriteFile(exe, []byte("old-binary"), 0o755))
	require.NoError(t, binaryupdate.Stage(exe, []byte("new-binary")))

	prev := runSyncPluginChild
	var gotExe string
	calls := 0
	runSyncPluginChild = func(e string) error {
		calls++
		gotExe = e
		return nil
	}
	t.Cleanup(func() { runSyncPluginChild = prev })

	applied, err := applyStagedAndSyncPlugin(exe)
	require.NoError(t, err)
	require.True(t, applied)
	require.Equal(t, 1, calls)
	require.Equal(t, exe, gotExe)

	got, err := os.ReadFile(exe)
	require.NoError(t, err)
	require.Equal(t, "new-binary", string(got))

	_, statErr := os.Stat(binaryupdate.StagedPath(exe))
	require.True(t, os.IsNotExist(statErr), "staged file should be removed after apply")
}

func TestApplyStagedAndSyncPlugin_NothingStaged_NoSync(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "subtask")
	require.NoError(t, os.WriteFile(exe, []byte("old-binary"), 0o755))

	prev := runSyncPluginChild
	calls := 0
	runSyncPluginChild = func(e string) error {
		calls++
		return nil
	}
	t.Cleanup(func() { runSyncPluginChild = prev })

	applied, err := applyStagedAndSyncPlugin(exe)
	require.NoError(t, err)
	require.False(t, applied)
	require.Zero(t, calls)
}

func TestApplyStagedAndSyncPlugin_ApplyFailure_NoSync(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("failure injection relies on unix directory permissions; Windows ignores read-only bits for renames")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "subtask")
	require.NoError(t, os.WriteFile(exe, []byte("old-binary"), 0o755))
	require.NoError(t, binaryupdate.Stage(exe, []byte("new-binary")))

	// Make the containing directory read-only so Apply's rename-into-place
	// fails after the staged file is already found and read.
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	prev := runSyncPluginChild
	calls := 0
	runSyncPluginChild = func(e string) error {
		calls++
		return nil
	}
	t.Cleanup(func() { runSyncPluginChild = prev })

	applied, err := applyStagedAndSyncPlugin(exe)
	require.Error(t, err)
	require.False(t, applied)
	require.Zero(t, calls)
}
