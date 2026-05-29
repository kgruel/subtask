package install

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAutoUpdateIfInstalled_DoesNotCreateWhenMissing(t *testing.T) {
	base := t.TempDir()

	res, err := AutoUpdateIfInstalled(base, "0.4.1")
	require.NoError(t, err)
	require.False(t, res.UpdatedSkill)
	require.False(t, res.UpdatedPlugin)

	_, err = os.Stat(SkillPath(base))
	require.ErrorIs(t, err, os.ErrNotExist)

	// Plugin must not be created when nothing was installed.
	_, err = os.Stat(PluginPath(base))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestAutoUpdateIfInstalled_RepairsDrift(t *testing.T) {
	base := t.TempDir()

	_, _, err := InstallTo(base)
	require.NoError(t, err)

	// Drift.
	require.NoError(t, os.WriteFile(SkillPath(base), []byte("different"), 0o644))

	res, err := AutoUpdateIfInstalled(base, "0.4.1")
	require.NoError(t, err)
	require.True(t, res.UpdatedSkill)

	got, err := os.ReadFile(SkillPath(base))
	require.NoError(t, err)
	require.Equal(t, Embedded(), got)
}

func TestAutoUpdateIfInstalled_RefreshesBinaryPlugin(t *testing.T) {
	base := t.TempDir()

	// Seed a binary-installed plugin, then drift it.
	_, err := InstallPluginBinaryTo(base, "0.4.1")
	require.NoError(t, err)
	drifted := filepath.Join(PluginPath(base), "hooks", "hooks.json")
	require.NoError(t, os.WriteFile(drifted, []byte("{}"), 0o644))

	res, err := AutoUpdateIfInstalled(base, "0.4.2")
	require.NoError(t, err)
	require.True(t, res.UpdatedPlugin)
}

func TestAutoUpdateIfInstalled_LeavesMarketplacePlugin(t *testing.T) {
	base := t.TempDir()

	// Marketplace-style plugin: manifest present, no ownership marker.
	pluginDir := PluginPath(base)
	require.NoError(t, os.MkdirAll(filepath.Join(pluginDir, ".claude-plugin"), 0o755))
	require.NoError(t, os.WriteFile(PluginManifestPath(pluginDir), []byte("{}"), 0o644))

	res, err := AutoUpdateIfInstalled(base, "0.4.2")
	require.NoError(t, err)
	require.False(t, res.UpdatedPlugin)
}
