package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/testutil"
)

func TestListCmd_Run_FromSubdir(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	subdir := filepath.Join(env.RootDir, "src")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	require.NoError(t, os.Chdir(subdir))

	stdout, stderr, err := captureStdoutStderr(t, (&ListCmd{}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.NotEmpty(t, stdout)
}
