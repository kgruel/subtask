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
