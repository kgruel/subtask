package logs

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeParser_ParseFile_EmitsUserAgentAndTool(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "sess.jsonl")
	data := "" +
		`{"type":"user","timestamp":"2026-01-13T22:55:27.897Z","sessionId":"sess","cwd":"/tmp/p","version":"2.1.6","message":{"role":"user","content":"hi"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-01-13T22:55:30.121Z","sessionId":"sess","cwd":"/tmp/p","version":"2.1.6","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"git status"}},{"type":"text","text":"done"}]}}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(data), 0600))

	p := &ClaudeParser{}
	var kinds []EntryKind
	info, err := p.ParseFile(path, func(e LogEntry) {
		kinds = append(kinds, e.Kind)
	})
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "sess", info.ID)

	// SessionStart should be synthesized once we see session metadata.
	require.Contains(t, kinds, KindSessionStart)
	require.Contains(t, kinds, KindUserMessage)
	require.Contains(t, kinds, KindToolCall)
	require.Contains(t, kinds, KindAgentMessage)
}

func TestClaudeParser_FindSessionFile_XDGPath(t *testing.T) {
	// Modern installs (or users who've migrated their config dir) keep
	// session JSONL under $XDG_CONFIG_HOME/claude/projects, not the legacy
	// ~/.claude/projects. Trace must find the file regardless.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", "") // exercise the default $HOME/.config path

	sessionID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	xdgProject := filepath.Join(tmp, ".config", "claude", "projects", "esc")
	require.NoError(t, os.MkdirAll(xdgProject, 0o700))
	want := filepath.Join(xdgProject, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(want, []byte(`{}`), 0o600))

	p := &ClaudeParser{}
	got, err := p.FindSessionFile(sessionID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestClaudeParser_FindSessionFile_LegacyPath(t *testing.T) {
	// Default-on-install today is ~/.claude/projects. Sessions there must
	// still be findable when XDG isn't populated.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")

	sessionID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	legacyProject := filepath.Join(tmp, ".claude", "projects", "esc")
	require.NoError(t, os.MkdirAll(legacyProject, 0o700))
	want := filepath.Join(legacyProject, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(want, []byte(`{}`), 0o600))

	p := &ClaudeParser{}
	got, err := p.FindSessionFile(sessionID)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestClaudeParser_FindSessionFile_PrefersXDGWhenBothExist(t *testing.T) {
	// If both roots happen to exist and contain the same session ID, XDG
	// wins. This is the kaygee box's case after a manual migration.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", "")

	sessionID := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	xdgProject := filepath.Join(tmp, ".config", "claude", "projects", "esc")
	legacyProject := filepath.Join(tmp, ".claude", "projects", "esc")
	require.NoError(t, os.MkdirAll(xdgProject, 0o700))
	require.NoError(t, os.MkdirAll(legacyProject, 0o700))
	xdgFile := filepath.Join(xdgProject, sessionID+".jsonl")
	legacyFile := filepath.Join(legacyProject, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(xdgFile, []byte(`{"loc":"xdg"}`), 0o600))
	require.NoError(t, os.WriteFile(legacyFile, []byte(`{"loc":"legacy"}`), 0o600))

	p := &ClaudeParser{}
	got, err := p.FindSessionFile(sessionID)
	require.NoError(t, err)
	require.Equal(t, xdgFile, got, "XDG location must be preferred when both exist")
}
