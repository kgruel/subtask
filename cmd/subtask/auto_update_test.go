package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/install"
)

func TestRunAutoUpdate_ProjectSkillOutdated_Warns(t *testing.T) {
	withOutputMode(t, false)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(autoUpdateEnvVar, "")

	repo := t.TempDir()
	gitCmd(t, repo, "init")

	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	projectSkill := filepath.Join(repo, ".claude", "skills", "subtask", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(projectSkill), 0o755))
	require.NoError(t, os.WriteFile(projectSkill, []byte("outdated"), 0o644))

	stdout, stderr, err := captureStdoutStderr(t, func() error {
		runAutoUpdate()
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, stdout)
	require.Equal(t, "warning: Project skill at "+filepath.Join(".claude", "skills", "subtask", "SKILL.md")+" is outdated. Run `subtask install --scope project` to update.\n", stderr)
}

func TestRunAutoUpdate_ProjectSkillUpToDate_Silent(t *testing.T) {
	withOutputMode(t, false)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(autoUpdateEnvVar, "")

	repo := t.TempDir()
	gitCmd(t, repo, "init")

	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	projectSkill := filepath.Join(repo, ".claude", "skills", "subtask", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(projectSkill), 0o755))
	require.NoError(t, os.WriteFile(projectSkill, install.Embedded(), 0o644))

	stdout, stderr, err := captureStdoutStderr(t, func() error {
		runAutoUpdate()
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, stdout)
	require.Empty(t, stderr)
}

func TestRunAutoUpdate_NoGitRepo_Silent(t *testing.T) {
	withOutputMode(t, false)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(autoUpdateEnvVar, "")

	dir := t.TempDir()

	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	// Even if a project-scope path exists here, project scope only applies inside a git repo.
	projectSkill := filepath.Join(dir, ".claude", "skills", "subtask", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(projectSkill), 0o755))
	require.NoError(t, os.WriteFile(projectSkill, []byte("outdated"), 0o644))

	stdout, stderr, err := captureStdoutStderr(t, func() error {
		runAutoUpdate()
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, stdout)
	require.Empty(t, stderr)
}

func TestRunAutoUpdate_InstallCommand_SuppressesStaleWarning(t *testing.T) {
	// When `subtask install` is running, runAutoUpdate fires before the install
	// write. The stale-skill warning must be suppressed so it doesn't appear
	// before the install success line.
	withOutputMode(t, false)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(autoUpdateEnvVar, "")

	repo := t.TempDir()
	gitCmd(t, repo, "init")

	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	projectSkill := filepath.Join(repo, ".claude", "skills", "subtask", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(projectSkill), 0o755))
	require.NoError(t, os.WriteFile(projectSkill, []byte("outdated"), 0o644))

	origArgs := os.Args
	os.Args = []string{"subtask", "install"}
	t.Cleanup(func() { os.Args = origArgs })

	stdout, stderr, err := captureStdoutStderr(t, func() error {
		runAutoUpdate()
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, stdout)
	require.Empty(t, stderr, "stale-skill warning must be suppressed during install")
}

func TestRunInternalPluginSync_RefreshesBinaryPlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, err := install.InstallPluginBinaryTo(home, "0.4.1")
	require.NoError(t, err)
	drifted := filepath.Join(install.PluginPath(home), "hooks", "hooks.json")
	require.NoError(t, os.WriteFile(drifted, []byte("{}"), 0o644))

	prevVersion := version
	version = "0.4.2"
	t.Cleanup(func() { version = prevVersion })

	require.NoError(t, runInternalPluginSync())

	got, err := os.ReadFile(drifted)
	require.NoError(t, err)
	require.NotEqual(t, "{}", string(got))
}

// TestRunInternalPluginSync_HomedirFailure_ReturnsError exercises the
// failure->non-zero-exit->parent-warning chain as far as the seams allow: if
// runInternalPluginSync can't even resolve a home directory, it must return
// an error rather than silently exiting 0 — otherwise a re-exec'd sync
// failure (see UpdateCmd.refreshPluginAfterSwap) would never surface, leaving
// the binary and plugin silently out of lockstep.
func TestRunInternalPluginSync_HomedirFailure_ReturnsError(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	err := runInternalPluginSync()
	require.Error(t, err)
}

func TestRunAutoUpdate_AutoUpdateDisabled_SkipsChecks(t *testing.T) {
	withOutputMode(t, false)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(autoUpdateEnvVar, "1")

	repo := t.TempDir()
	gitCmd(t, repo, "init")

	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(repo))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	projectSkill := filepath.Join(repo, ".claude", "skills", "subtask", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(projectSkill), 0o755))
	require.NoError(t, os.WriteFile(projectSkill, []byte("outdated"), 0o644))

	stdout, stderr, err := captureStdoutStderr(t, func() error {
		runAutoUpdate()
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, stdout)
	require.Empty(t, stderr)
}
