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
