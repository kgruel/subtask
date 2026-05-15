package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/install"
	"github.com/kgruel/subtask/pkg/render"
)

func TestInstallStatusUninstall_UserScope_NoPrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SUBTASK_DIR", filepath.Join(home, ".subtask"))

	cwd := t.TempDir()
	prev, _ := os.Getwd()
	require.NoError(t, os.Chdir(cwd))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	// Ensure at least one harness is "available" so `subtask install --no-prompt`
	// can write a usable ~/.subtask/config.json.
	binDir := filepath.Join(cwd, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	_ = writeFakeCLI(t, binDir, "codex")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	withOutputMode(t, false)
	render.Pretty = false

	stdout, stderr, err := captureStdoutStderr(t, (&StatusCmd{}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Skill installed: no")
	require.NotContains(t, stdout, "Plugin installed")

	_, stderr, err = captureStdoutStderr(t, (&InstallCmd{NoPrompt: true}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)

	stdout, stderr, err = captureStdoutStderr(t, (&StatusCmd{}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Skill installed: yes")
	require.NotContains(t, stdout, "Plugin installed")

	_, stderr, err = captureStdoutStderr(t, (&UninstallCmd{}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)

	stdout, stderr, err = captureStdoutStderr(t, (&StatusCmd{}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)
	require.Contains(t, stdout, "Skill installed: no")
	require.NotContains(t, stdout, "Plugin installed")
}

// TestInstallCmd_M10_ExactlyOneSuccessLine verifies that running `subtask install`
// against a stale skill emits exactly one "✓" success line total, even though
// runAutoUpdate also runs before the command. Before the fix, both emitted a line.
func TestInstallCmd_M10_ExactlyOneSuccessLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SUBTASK_DIR", filepath.Join(home, ".subtask"))
	t.Setenv(autoUpdateEnvVar, "")

	// Pre-install a stale skill so both auto-update and install would want to write.
	skillPath := install.SkillPath(home)
	require.NoError(t, os.MkdirAll(filepath.Dir(skillPath), 0o755))
	require.NoError(t, os.WriteFile(skillPath, []byte("stale content"), 0o644))

	cwd := t.TempDir()
	prev, _ := os.Getwd()
	require.NoError(t, os.Chdir(cwd))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	binDir := filepath.Join(cwd, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	_ = writeFakeCLI(t, binDir, "codex")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	origArgs := os.Args
	os.Args = []string{"subtask", "install"}
	t.Cleanup(func() { os.Args = origArgs })

	withOutputMode(t, false)
	render.Pretty = false

	// Capture runAutoUpdate (normally writes to stderr).
	_, autoStderr, err := captureStdoutStderr(t, func() error {
		runAutoUpdate()
		return nil
	})
	require.NoError(t, err)

	// Capture InstallCmd output.
	installStdout, _, err := captureStdoutStderr(t, (&InstallCmd{NoPrompt: true, SkillOnly: true}).Run)
	require.NoError(t, err)

	// Auto-update must emit nothing — the install guard should suppress it.
	require.Empty(t, autoStderr, "runAutoUpdate must not emit any output during install")

	// Install stdout must contain exactly one skill outcome line. The M10 bug
	// produced two: "Updated skill to latest version" (from auto-update) then
	// "Skill already up to date" (from install). With the fix, auto-update is
	// suppressed during install so only install's line appears.
	var skillLines []string
	for line := range strings.SplitSeq(installStdout+autoStderr, "\n") {
		if strings.Contains(line, "skill to latest") ||
			strings.Contains(line, "Installed skill") ||
			strings.Contains(line, "Skill already up to date") {
			skillLines = append(skillLines, line)
		}
	}
	require.Len(t, skillLines, 1, "expected exactly one skill outcome line, got:\nauto-update stderr: %q\ninstall stdout: %q", autoStderr, installStdout)
}

// TestInstallCmd_M9_RepoCheckoutUsesRepoSKILL verifies that when install runs from
// inside a subtask checkout, it writes the repo SKILL file (not the binary embed),
// and the success message includes the repo file path.
func TestInstallCmd_M9_RepoCheckoutUsesRepoSKILL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SUBTASK_DIR", filepath.Join(home, ".subtask"))

	// Build a fake subtask checkout in a temp dir.
	repoRoot := t.TempDir()
	repoSKILL := filepath.Join(repoRoot, "pkg", "install", "SKILL.md")
	repoContent := []byte("# repo skill — distinct from embed\n")
	require.NoError(t, os.MkdirAll(filepath.Dir(repoSKILL), 0o755))
	require.NoError(t, os.WriteFile(repoRoot+"/go.mod", []byte("module github.com/kgruel/subtask\n\ngo 1.24\n"), 0o644))
	require.NoError(t, os.WriteFile(repoSKILL, repoContent, 0o644))

	binDir := filepath.Join(repoRoot, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	_ = writeFakeCLI(t, binDir, "codex")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	prev, _ := os.Getwd()
	require.NoError(t, os.Chdir(repoRoot))
	t.Cleanup(func() { _ = os.Chdir(prev) })

	withOutputMode(t, false)
	render.Pretty = false

	stdout, _, err := captureStdoutStderr(t, (&InstallCmd{NoPrompt: true, SkillOnly: true}).Run)
	require.NoError(t, err)

	// Installed file must match the repo content, not the binary embed.
	skillPath := install.SkillPath(home)
	got, err := os.ReadFile(skillPath)
	require.NoError(t, err)
	require.Equal(t, repoContent, got, "installed skill should be repo content, not binary embed")
	require.NotEqual(t, install.Embedded(), got, "installed skill must NOT be the binary embed")

	// Success message must mention the repo SKILL path.
	require.Contains(t, stdout, "pkg/install/SKILL.md", "success message should include repo SKILL path")
}
