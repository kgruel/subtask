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
