package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeManifest(t *testing.T, dir string) {
	t.Helper()
	manifestDir := filepath.Join(dir, ".claude-plugin")
	require.NoError(t, os.MkdirAll(manifestDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(manifestDir, "plugin.json"), []byte(`{"name":"subtask"}`), 0o644))
}

func TestGetPluginStatus_Absent(t *testing.T) {
	base := t.TempDir()
	st, err := GetPluginStatusFor(base)
	require.NoError(t, err)
	assert.False(t, st.Exists)
	assert.Equal(t, filepath.Join(base, ".claude", "plugins", "subtask"), st.Path)
}

func TestInstallPluginDev_CreatesSymlink(t *testing.T) {
	base := t.TempDir()
	source := t.TempDir()
	writeManifest(t, source)

	res, err := InstallPluginDevTo(base, source)
	require.NoError(t, err)
	assert.Equal(t, "created", res.Action)
	assert.Equal(t, source, res.SourceDir)

	st, err := GetPluginStatusFor(base)
	require.NoError(t, err)
	assert.True(t, st.Exists)
	assert.True(t, st.IsSymlink)
	assert.Equal(t, source, st.SymlinkTarget)
	assert.True(t, st.HasManifest)
}

func TestInstallPluginDev_IdempotentNoop(t *testing.T) {
	base := t.TempDir()
	source := t.TempDir()
	writeManifest(t, source)

	_, err := InstallPluginDevTo(base, source)
	require.NoError(t, err)

	res, err := InstallPluginDevTo(base, source)
	require.NoError(t, err)
	assert.Equal(t, "noop", res.Action)
}

func TestInstallPluginDev_UpdatesWrongSymlink(t *testing.T) {
	base := t.TempDir()
	oldSource := t.TempDir()
	newSource := t.TempDir()
	writeManifest(t, oldSource)
	writeManifest(t, newSource)

	_, err := InstallPluginDevTo(base, oldSource)
	require.NoError(t, err)

	res, err := InstallPluginDevTo(base, newSource)
	require.NoError(t, err)
	assert.Equal(t, "updated", res.Action)

	st, err := GetPluginStatusFor(base)
	require.NoError(t, err)
	assert.Equal(t, newSource, st.SymlinkTarget)
}

func TestInstallPluginDev_RefusesToClobberRealDir(t *testing.T) {
	base := t.TempDir()
	source := t.TempDir()
	writeManifest(t, source)

	target := PluginPath(base)
	require.NoError(t, os.MkdirAll(target, 0o755))

	_, err := InstallPluginDevTo(base, source)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a symlink")
}

func TestInstallPluginDev_RejectsSourceWithoutManifest(t *testing.T) {
	base := t.TempDir()
	source := t.TempDir()
	// No manifest written.

	_, err := InstallPluginDevTo(base, source)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a plugin")
}

func TestInstallPluginDev_RejectsMissingSource(t *testing.T) {
	base := t.TempDir()
	_, err := InstallPluginDevTo(base, filepath.Join(base, "nonexistent"))
	require.Error(t, err)
}

// --- Binary install/uninstall tests ---

func TestInstallPluginBinaryTo_FreshInstall(t *testing.T) {
	base := t.TempDir()

	res, err := InstallPluginBinaryTo(base, "0.4.1")
	require.NoError(t, err)
	assert.Equal(t, "installed", res.Action)

	pluginDir := PluginPath(base)

	// Marker file present.
	markerPath := filepath.Join(pluginDir, ".subtask-binary-installed")
	content, err := os.ReadFile(markerPath)
	require.NoError(t, err)
	assert.Equal(t, "0.4.1\n", string(content))

	// plugin.json present.
	_, err = os.Stat(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"))
	require.NoError(t, err)

	// .sh files are executable.
	info, err := os.Stat(filepath.Join(pluginDir, "scripts", "stop-unread.sh"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o111, ".sh file should be executable")

	// GetPluginStatusFor reflects IsBinaryInstalled.
	st, err := GetPluginStatusFor(base)
	require.NoError(t, err)
	assert.True(t, st.IsBinaryInstalled)
	assert.True(t, st.HasManifest)
}

func TestInstallPluginBinaryTo_Noop(t *testing.T) {
	base := t.TempDir()

	_, err := InstallPluginBinaryTo(base, "0.4.1")
	require.NoError(t, err)

	// Second install with same embedded content → noop.
	res, err := InstallPluginBinaryTo(base, "0.4.1")
	require.NoError(t, err)
	assert.Equal(t, "noop", res.Action)
}

func TestInstallPluginBinaryTo_UpdatesWhenFilesDiffer(t *testing.T) {
	base := t.TempDir()

	_, err := InstallPluginBinaryTo(base, "0.4.1")
	require.NoError(t, err)

	// Drift a file.
	drifted := filepath.Join(PluginPath(base), "hooks", "hooks.json")
	require.NoError(t, os.WriteFile(drifted, []byte("{}"), 0o644))

	res, err := InstallPluginBinaryTo(base, "0.4.2")
	require.NoError(t, err)
	assert.Equal(t, "updated", res.Action)
}

func TestInstallPluginBinaryTo_LeavesMarketplaceDir(t *testing.T) {
	base := t.TempDir()
	pluginDir := PluginPath(base)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	// Write a manifest but no ownership marker.
	writeManifest(t, pluginDir)

	res, err := InstallPluginBinaryTo(base, "0.4.1")
	require.NoError(t, err)
	assert.Equal(t, "marketplace", res.Action)

	// Marker was not created.
	_, err = os.Stat(filepath.Join(pluginDir, ".subtask-binary-installed"))
	assert.True(t, os.IsNotExist(err))
}

func TestInstallPluginBinaryTo_LeavesDevSymlink(t *testing.T) {
	base := t.TempDir()
	source := t.TempDir()
	writeManifest(t, source)
	_, err := InstallPluginDevTo(base, source)
	require.NoError(t, err)

	res, err := InstallPluginBinaryTo(base, "0.4.1")
	require.NoError(t, err)
	assert.Equal(t, "dev_link", res.Action)

	// Symlink still intact.
	st, err := GetPluginStatusFor(base)
	require.NoError(t, err)
	assert.True(t, st.IsSymlink)
}

func TestInstallPluginBinaryTo_StraySymlink(t *testing.T) {
	base := t.TempDir()
	pluginDir := PluginPath(base)
	require.NoError(t, os.MkdirAll(filepath.Dir(pluginDir), 0o755))
	// Symlink to non-existent target.
	require.NoError(t, os.Symlink("/nonexistent/path/subtask", pluginDir))

	res, err := InstallPluginBinaryTo(base, "0.4.1")
	require.NoError(t, err)
	assert.Equal(t, "stray", res.Action)
}

func TestUninstallPluginBinaryFrom_RemovesBinaryInstall(t *testing.T) {
	base := t.TempDir()

	_, err := InstallPluginBinaryTo(base, "0.4.1")
	require.NoError(t, err)

	res, err := UninstallPluginBinaryFrom(base)
	require.NoError(t, err)
	assert.Equal(t, "removed", res.Action)

	_, err = os.Stat(PluginPath(base))
	assert.True(t, os.IsNotExist(err))
}

func TestUninstallPluginBinaryFrom_LeavesMarketplaceDir(t *testing.T) {
	base := t.TempDir()
	pluginDir := PluginPath(base)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	writeManifest(t, pluginDir)

	res, err := UninstallPluginBinaryFrom(base)
	require.NoError(t, err)
	assert.Equal(t, "marketplace", res.Action)

	// Marketplace dir still there.
	_, err = os.Stat(pluginDir)
	require.NoError(t, err)
}

func TestUninstallPluginBinaryFrom_PreservesDevSymlink(t *testing.T) {
	base := t.TempDir()
	source := t.TempDir()
	writeManifest(t, source)
	_, err := InstallPluginDevTo(base, source)
	require.NoError(t, err)

	res, err := UninstallPluginBinaryFrom(base)
	require.NoError(t, err)
	assert.Equal(t, "dev_link", res.Action)
	assert.Contains(t, res.Note, "preserved")

	// Symlink still there.
	_, err = os.Lstat(PluginPath(base))
	require.NoError(t, err)
}

func TestUninstallPluginBinaryFrom_PreservesStraySymlink(t *testing.T) {
	base := t.TempDir()
	pluginPath := PluginPath(base)
	require.NoError(t, os.MkdirAll(filepath.Dir(pluginPath), 0o755))
	// Symlink that points to a non-existent target → no manifest.
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "missing"), pluginPath))

	res, err := UninstallPluginBinaryFrom(base)
	require.NoError(t, err)
	assert.Equal(t, "stray", res.Action)
	assert.Contains(t, res.Note, "preserved")

	// Stray symlink left in place — user removes manually.
	_, err = os.Lstat(pluginPath)
	require.NoError(t, err)
}

func TestUninstallPluginBinaryFrom_NothingWhenAbsent(t *testing.T) {
	base := t.TempDir()

	res, err := UninstallPluginBinaryFrom(base)
	require.NoError(t, err)
	assert.Equal(t, "nothing", res.Action)
}

func TestGetPluginStatusFor_IsBinaryInstalled(t *testing.T) {
	base := t.TempDir()

	// Not installed → false.
	st, err := GetPluginStatusFor(base)
	require.NoError(t, err)
	assert.False(t, st.IsBinaryInstalled)

	_, err = InstallPluginBinaryTo(base, "0.4.1")
	require.NoError(t, err)

	// Installed → true.
	st, err = GetPluginStatusFor(base)
	require.NoError(t, err)
	assert.True(t, st.IsBinaryInstalled)
}
