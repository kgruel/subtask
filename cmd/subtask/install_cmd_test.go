package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/render"
)

func TestInstallStatusUninstall_UserScope_NoPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cwd := t.TempDir()
	prev, _ := os.Getwd()
	require.NoError(t, os.Chdir(cwd))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	withOutputMode(t, false)
	render.Pretty = false

	stdout, stderr, err := captureStdoutStderr(t, (&StatusCmd{}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Skill installed: no")
	require.Contains(t, stdout, "Plugin installed: no")

	_, stderr, err = captureStdoutStderr(t, (&InstallCmd{NoPrompt: true}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)

	stdout, stderr, err = captureStdoutStderr(t, (&StatusCmd{}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Skill installed: yes")
	require.Contains(t, stdout, "Plugin installed: yes")
	require.Contains(t, stdout, "Plugin enabled: yes")

	_, stderr, err = captureStdoutStderr(t, (&UninstallCmd{}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)

	stdout, stderr, err = captureStdoutStderr(t, (&StatusCmd{}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Skill installed: no")
	require.Contains(t, stdout, "Plugin installed: no")
}
