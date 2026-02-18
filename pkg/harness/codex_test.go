package harness

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexDuplicateSession_CopiesAndRewritesSessionMetaID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp) // Windows uses USERPROFILE instead of HOME

	sessionID := "11111111-1111-1111-1111-111111111111"
	srcDir := filepath.Join(tmp, ".codex", "sessions", "2026", "01", "01")
	require.NoError(t, os.MkdirAll(srcDir, 0700))

	src := filepath.Join(srcDir, "rollout-2026-01-01T00-00-00-"+sessionID+".jsonl")
	srcData := strings.Join([]string{
		`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"` + sessionID + `","cwd":"/tmp/x","cli_version":"0.80.0"}}`,
		`{"timestamp":"2026-01-01T00:00:01Z","type":"event_msg","payload":{"type":"agent_message","message":"hi"}}`,
	}, "\n") + "\n"
	require.NoError(t, os.WriteFile(src, []byte(srcData), 0600))

	newSessionID, err := duplicateCodexSession(sessionID, "/tmp/old", "/tmp/new")
	require.NoError(t, err)
	require.NotEmpty(t, newSessionID)
	require.NotEqual(t, sessionID, newSessionID)

	dst := filepath.Join(srcDir, strings.Replace(filepath.Base(src), sessionID, newSessionID, 1))
	_, err = os.Stat(dst)
	require.NoError(t, err)

	readFirstLine := func(path string) map[string]any {
		f, err := os.Open(path)
		require.NoError(t, err)
		defer f.Close()
		sc := bufio.NewScanner(f)
		require.True(t, sc.Scan())
		var m map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &m))
		return m
	}

	srcFirst := readFirstLine(src)
	require.Equal(t, "session_meta", srcFirst["type"])
	srcPayload, ok := srcFirst["payload"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, sessionID, srcPayload["id"])

	dstFirst := readFirstLine(dst)
	require.Equal(t, "session_meta", dstFirst["type"])
	dstPayload, ok := dstFirst["payload"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, newSessionID, dstPayload["id"])
	require.Equal(t, "0.80.0", dstPayload["cli_version"])
}
