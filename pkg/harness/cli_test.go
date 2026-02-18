package harness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommandForCLI_FindsClaudeInClaudeLocalDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts are not portable on windows")
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp) // Windows uses USERPROFILE instead of HOME
	t.Setenv("PATH", t.TempDir())

	claudePath := filepath.Join(tmp, ".claude", "local", "claude")
	require.NoError(t, os.MkdirAll(filepath.Dir(claudePath), 0700))
	require.NoError(t, os.WriteFile(claudePath, []byte("#!/bin/sh\necho ok\n"), 0700))

	cmd, err := commandForCLI(context.Background(), cliSpec{Exec: "claude"}, []string{"--version"})
	require.NoError(t, err)
	require.Equal(t, claudePath, cmd.Path)

	out, err := cmd.Output()
	require.NoError(t, err)
	require.Equal(t, "ok\n", string(out))
}

func TestCommandForCLI_FindsOpenCodeInOpenCodeBinDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts are not portable on windows")
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp) // Windows uses USERPROFILE instead of HOME
	t.Setenv("PATH", t.TempDir())

	opencodePath := filepath.Join(tmp, ".opencode", "bin", "opencode")
	require.NoError(t, os.MkdirAll(filepath.Dir(opencodePath), 0700))
	require.NoError(t, os.WriteFile(opencodePath, []byte("#!/bin/sh\necho ok\n"), 0700))

	cmd, err := commandForCLI(context.Background(), cliSpec{Exec: "opencode"}, []string{"--version"})
	require.NoError(t, err)
	require.Equal(t, opencodePath, cmd.Path)

	out, err := cmd.Output()
	require.NoError(t, err)
	require.Equal(t, "ok\n", string(out))
}

func TestCommandForCLI_ResolvesViaShellAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell alias resolution is not portable on windows")
	}

	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("SHELL", bashPath)
	t.Setenv("PATH", t.TempDir())

	// bash login shells don't load ~/.bashrc automatically, so source it explicitly.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".bash_profile"), []byte(`. "$HOME/.bashrc"`+"\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, ".bashrc"), []byte(`alias fakecmd='echo shell-ok'`+"\n"), 0600))

	cmd, err := commandForCLI(context.Background(), cliSpec{Exec: "fakecmd"}, nil)
	require.NoError(t, err)
	require.Equal(t, bashPath, cmd.Path)

	out, err := cmd.Output()
	require.NoError(t, err)
	require.Equal(t, "shell-ok\n", string(out))
}
