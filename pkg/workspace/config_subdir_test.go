package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/testutil"
	"github.com/zippoxer/subtask/pkg/workspace"
)

func TestListWorkspaces_FromSubdir_UsesProjectRoot(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	subdir := filepath.Join(env.RootDir, "src")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	require.NoError(t, os.Chdir(subdir))

	workspaces, err := workspace.ListWorkspaces()
	require.NoError(t, err)

	var found bool
	for _, ws := range workspaces {
		if ws.Path == env.Workspaces[0] {
			found = true
			break
		}
	}
	require.True(t, found, "expected workspace created for project root to be discovered from a subdir")
}
