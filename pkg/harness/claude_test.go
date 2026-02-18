package harness

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseClaudeStream_SetsSessionAndToolCalls(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","cwd":"/tmp/x","session_id":"sess-1"}`,
		`{"type":"assistant","session_id":"sess-1","message":{"role":"assistant","content":[{"type":"tool_use","name":"Write","input":{"file_path":"/tmp/x/a.txt","content":"x"}},{"type":"text","text":"ok"}]}}`,
		`{"type":"result","subtype":"success","session_id":"sess-1","result":"final"}`,
	}, "\n")

	var gotSession string
	toolCalls := 0
	res := &Result{}

	err := parseClaudeStream(bytes.NewBufferString(stream), res, Callbacks{
		OnSessionStart: func(sessionID string) { gotSession = sessionID },
		OnToolCall:     func(time.Time) { toolCalls++ },
	})
	require.NoError(t, err)
	require.Equal(t, "sess-1", gotSession)
	require.Equal(t, "sess-1", res.SessionID)
	require.True(t, res.PromptDelivered)
	require.True(t, res.AgentReplied)
	require.Equal(t, "final", res.Reply)
	require.Equal(t, 1, toolCalls)
}

func TestClaudeMigrateSession_RewritesPathsAndVerifies(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp) // Windows uses USERPROFILE instead of HOME

	oldCwd := "/private/tmp/ws-old"
	newCwd := "/private/tmp/ws-new"
	sessionID := "11111111-1111-1111-1111-111111111111"

	oldProject := filepath.Join(tmp, ".claude", "projects", escapeClaudeProjectDir(oldCwd))
	newProject := filepath.Join(tmp, ".claude", "projects", escapeClaudeProjectDir(newCwd))

	require.NoError(t, os.MkdirAll(oldProject, 0700))
	src := filepath.Join(oldProject, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(src, []byte(`{"cwd":"`+oldCwd+`","x":"`+oldCwd+`"}`+"\n"), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(oldProject, sessionID), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(oldProject, sessionID, "meta.txt"), []byte("from "+oldCwd), 0600))

	require.NoError(t, migrateClaudeSession(sessionID, oldCwd, newCwd))

	dst := filepath.Join(newProject, sessionID+".jsonl")
	out, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.NotContains(t, string(out), oldCwd)
	require.Contains(t, string(out), newCwd)

	// Migration should remove the source artifacts.
	_, err = os.Stat(src)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(oldProject, sessionID))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestClaudeDuplicateSession_CopiesAndRewritesSessionID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp) // Windows uses USERPROFILE instead of HOME

	oldCwd := "/private/tmp/ws-old"
	newCwd := "/private/tmp/ws-new"
	sessionID := "11111111-1111-1111-1111-111111111111"

	oldProject := filepath.Join(tmp, ".claude", "projects", escapeClaudeProjectDir(oldCwd))
	newProject := filepath.Join(tmp, ".claude", "projects", escapeClaudeProjectDir(newCwd))

	require.NoError(t, os.MkdirAll(oldProject, 0700))
	src := filepath.Join(oldProject, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(src, []byte(`{"sessionId":"`+sessionID+`","cwd":"`+oldCwd+`","x":"`+oldCwd+`"}`+"\n"), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(oldProject, sessionID), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(oldProject, sessionID, "meta.txt"), []byte("from "+oldCwd+" "+sessionID), 0600))

	newSessionID, err := duplicateClaudeSession(sessionID, oldCwd, newCwd)
	require.NoError(t, err)
	require.NotEmpty(t, newSessionID)
	require.NotEqual(t, sessionID, newSessionID)

	// Original remains.
	_, err = os.Stat(src)
	require.NoError(t, err)

	// Duplicated session written under the new project with a new ID.
	dst := filepath.Join(newProject, newSessionID+".jsonl")
	out, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.NotContains(t, string(out), oldCwd)
	require.Contains(t, string(out), newCwd)
	require.NotContains(t, string(out), sessionID)
	require.Contains(t, string(out), newSessionID)

	meta, err := os.ReadFile(filepath.Join(newProject, newSessionID, "meta.txt"))
	require.NoError(t, err)
	require.NotContains(t, string(meta), oldCwd)
	require.Contains(t, string(meta), newCwd)
	require.NotContains(t, string(meta), sessionID)
	require.Contains(t, string(meta), newSessionID)
}
