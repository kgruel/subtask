package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/install"
)

func TestInstall_UserScope_CreatesSkillPluginAndObjectSettings(t *testing.T) {
	bin := buildSubtask(t)

	home := t.TempDir()
	cwd := t.TempDir()

	out := runSubtask(t, bin, cwd, home, "install", "--no-prompt")
	require.Contains(t, out, "Installed skill")
	require.Contains(t, out, "Installed plugin")

	// Skill path.
	skillPath := filepath.Join(home, ".claude", "skills", "subtask", "SKILL.md")
	gotSkill, err := os.ReadFile(skillPath)
	require.NoError(t, err)
	require.Equal(t, install.Embedded(), gotSkill)

	// Plugin files + exec bit.
	pluginDir := filepath.Join(home, ".claude", "plugins", "subtask")
	require.FileExists(t, filepath.Join(pluginDir, ".claude-plugin", "plugin.json"))
	require.FileExists(t, filepath.Join(pluginDir, "hooks", "hooks.json"))
	scriptPath := filepath.Join(pluginDir, "scripts", "skill-reminder.sh")
	info, err := os.Stat(scriptPath)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.NotZero(t, info.Mode().Perm()&0o111, "should be executable on Unix")
	}

	// Settings.json: enabledPlugins must be object.
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	var settings map[string]any
	require.NoError(t, readJSON(settingsPath, &settings))
	enabled, ok := settings["enabledPlugins"].(map[string]any)
	require.True(t, ok, "enabledPlugins should be an object")
	require.Equal(t, true, enabled["subtask"])

	// Idempotent: second install shouldn't break settings or content.
	out2 := runSubtask(t, bin, cwd, home, "install", "--no-prompt")
	require.Contains(t, out2, "Skill already up to date")
	require.Contains(t, out2, "Plugin already up to date")
	require.NoError(t, readJSON(settingsPath, &settings))
	enabled, ok = settings["enabledPlugins"].(map[string]any)
	require.True(t, ok, "enabledPlugins should be an object")
	require.Equal(t, true, enabled["subtask"])
}

func TestInstall_Settings_ObjectFormatPreserved(t *testing.T) {
	bin := buildSubtask(t)
	home := t.TempDir()
	cwd := t.TempDir()

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(settingsPath), 0o755))
	require.NoError(t, os.WriteFile(settingsPath, []byte(`{
  "enabledPlugins": { "other": true },
  "keep": { "nested": 123 }
}
`), 0o644))

	_ = runSubtask(t, bin, cwd, home, "install", "--no-prompt", "--plugin")

	var settings map[string]any
	require.NoError(t, readJSON(settingsPath, &settings))

	enabled, ok := settings["enabledPlugins"].(map[string]any)
	require.True(t, ok, "enabledPlugins should remain an object")
	require.Equal(t, true, enabled["other"])
	require.Equal(t, true, enabled["subtask"])

	keep, ok := settings["keep"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(123), keep["nested"])
}

func TestInstall_Settings_ArrayFormatConvertedToObject(t *testing.T) {
	bin := buildSubtask(t)
	home := t.TempDir()
	cwd := t.TempDir()

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(settingsPath), 0o755))
	require.NoError(t, os.WriteFile(settingsPath, []byte(`{"enabledPlugins":["other"]}`+"\n"), 0o644))

	_ = runSubtask(t, bin, cwd, home, "install", "--no-prompt", "--plugin")

	var settings map[string]any
	require.NoError(t, readJSON(settingsPath, &settings))
	enabled, ok := settings["enabledPlugins"].(map[string]any)
	require.True(t, ok, "enabledPlugins should be converted to an object")
	require.Equal(t, true, enabled["other"])
	require.Equal(t, true, enabled["subtask"])
}

func TestInstall_Settings_MalformedJSON_BackupsAndCreatesFreshObject(t *testing.T) {
	bin := buildSubtask(t)
	home := t.TempDir()
	cwd := t.TempDir()

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(settingsPath), 0o755))
	require.NoError(t, os.WriteFile(settingsPath, []byte("{not json"), 0o644))

	out := runSubtask(t, bin, cwd, home, "install", "--no-prompt", "--plugin")
	require.Contains(t, out, "Rewrote malformed settings.json")

	// Backup should exist (exact suffix may include timestamp).
	matches, err := filepath.Glob(settingsPath + ".bak*")
	require.NoError(t, err)
	require.NotEmpty(t, matches)

	var settings map[string]any
	require.NoError(t, readJSON(settingsPath, &settings))
	enabled, ok := settings["enabledPlugins"].(map[string]any)
	require.True(t, ok, "enabledPlugins should be an object")
	require.Equal(t, true, enabled["subtask"])
}

func TestUninstall_RemovesPluginFromEnabledPlugins(t *testing.T) {
	bin := buildSubtask(t)
	home := t.TempDir()
	cwd := t.TempDir()

	_ = runSubtask(t, bin, cwd, home, "install", "--no-prompt")

	settingsPath := filepath.Join(home, ".claude", "settings.json")
	var settings map[string]any
	require.NoError(t, readJSON(settingsPath, &settings))

	enabled := settings["enabledPlugins"].(map[string]any)
	enabled["other"] = true
	settings["enabledPlugins"] = enabled
	b, err := json.MarshalIndent(settings, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(settingsPath, append(b, '\n'), 0o644))

	_ = runSubtask(t, bin, cwd, home, "uninstall", "--plugin")

	require.NoError(t, readJSON(settingsPath, &settings))
	enabled, ok := settings["enabledPlugins"].(map[string]any)
	require.True(t, ok, "enabledPlugins should be an object")
	require.Nil(t, enabled["subtask"])
	require.Equal(t, true, enabled["other"])
}

func TestInstall_ProjectScope_UsesRepoRoot(t *testing.T) {
	bin := buildSubtask(t)

	home := t.TempDir()
	repo := t.TempDir()
	initGitRepo(t, repo)

	out := runSubtask(t, bin, repo, home, "install", "--no-prompt", "--scope", "project")
	require.Contains(t, out, "Installed skill")
	require.Contains(t, out, "Installed plugin")

	require.FileExists(t, filepath.Join(repo, ".claude", "skills", "subtask", "SKILL.md"))
	require.FileExists(t, filepath.Join(repo, ".claude", "plugins", "subtask", ".claude-plugin", "plugin.json"))
}

func TestAutoUpdate_RepairsDriftOnlyWhenInstalled(t *testing.T) {
	bin := buildSubtask(t)
	home := t.TempDir()
	cwd := t.TempDir()

	// Not installed: running any command should not create files.
	_ = runSubtask(t, bin, cwd, home, "--version")
	_, err := os.Stat(filepath.Join(home, ".claude", "skills", "subtask", "SKILL.md"))
	require.ErrorIs(t, err, os.ErrNotExist)

	// Install, then drift, then run status to trigger auto-update.
	_ = runSubtask(t, bin, cwd, home, "install", "--no-prompt")
	skillPath := filepath.Join(home, ".claude", "skills", "subtask", "SKILL.md")
	require.NoError(t, os.WriteFile(skillPath, []byte("different"), 0o644))
	pluginHookPath := filepath.Join(home, ".claude", "plugins", "subtask", "hooks", "hooks.json")
	require.NoError(t, os.WriteFile(pluginHookPath, []byte(`{}`), 0o644))

	out := runSubtask(t, bin, cwd, home, "status")
	require.Contains(t, out, "Updated skill to latest version")
	require.Contains(t, out, "Updated plugin to latest version")

	gotSkill, err := os.ReadFile(skillPath)
	require.NoError(t, err)
	require.Equal(t, install.Embedded(), gotSkill)
}

func runSubtask(t *testing.T, bin string, dir string, home string, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if len(kv) >= 5 && kv[:5] == "HOME=" {
			continue
		}
		if len(kv) >= 12 && kv[:12] == "USERPROFILE=" {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"HOME="+home,
		"USERPROFILE="+home, // windows
	)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
	return string(out)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "init", "-b", "main")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test User")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0o644))
	run(t, dir, "git", "add", ".")
	run(t, dir, "git", "commit", "-m", "Initial commit")
}

func TestInstallCLI_UsesWindowsExeName(t *testing.T) {
	// Guard: buildSubtask() already handles windows suffix; keep this to ensure
	// the helper stays correct if modified.
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	bin := buildSubtask(t)
	require.Contains(t, filepath.Base(bin), ".exe")
}
