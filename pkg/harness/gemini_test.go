package harness

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseGeminiStream_SessionAccumulatedReplyAndTools(t *testing.T) {
	input := strings.Join([]string{
		`not json`,
		`{"type":"init","timestamp":"2026-04-28T22:44:32Z","session_id":"sess-g1","model":"auto-gemini-3"}`,
		`{"type":"message","timestamp":"2026-04-28T22:44:32Z","role":"user","content":"ignore me"}`,
		`{"type":"message","timestamp":"2026-04-28T22:44:35Z","role":"assistant","content":"Hello ","delta":true}`,
		`{"type":"tool_use","timestamp":"2026-04-28T22:44:35Z","tool_name":"read_file","tool_id":"read_file_1","parameters":{"file_path":"x.txt"}}`,
		`{"type":"tool_result","timestamp":"2026-04-28T22:44:35Z","tool_id":"read_file_1","status":"success","output":"x"}`,
		`{"type":"message","timestamp":"2026-04-28T22:44:35Z","role":"assistant","content":"world!","delta":true}`,
		`{"type":"result","timestamp":"2026-04-28T22:44:36Z","status":"success","stats":{"total_tokens":42}}`,
	}, "\n") + "\n"

	var started []string
	toolCalls := 0
	res := &Result{}

	err := parseGeminiStream(strings.NewReader(input), res, Callbacks{
		OnSessionStart: func(sessionID string) {
			started = append(started, sessionID)
		},
		OnToolCall: func(_ time.Time) { toolCalls++ },
	})
	require.NoError(t, err)
	require.Equal(t, []string{"sess-g1"}, started)
	require.Equal(t, "sess-g1", res.SessionID)
	require.True(t, res.PromptDelivered)
	require.True(t, res.AgentReplied)
	require.Equal(t, "Hello world!", res.Reply)
	require.Equal(t, 1, toolCalls)
	require.Empty(t, res.Error)
}

func TestParseGeminiStream_NonDeltaReplaces(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"init","session_id":"sess-g2","model":"x"}`,
		`{"type":"message","role":"assistant","content":"first chunk","delta":true}`,
		`{"type":"message","role":"assistant","content":"final answer"}`,
	}, "\n") + "\n"

	res := &Result{}
	err := parseGeminiStream(strings.NewReader(input), res, Callbacks{})
	require.NoError(t, err)
	require.Equal(t, "final answer", res.Reply)
}

func TestParseGeminiStream_ErrorStatus(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"init","session_id":"sess-g3"}`,
		`{"type":"result","status":"error"}`,
	}, "\n") + "\n"

	res := &Result{}
	err := parseGeminiStream(strings.NewReader(input), res, Callbacks{})
	require.NoError(t, err)
	require.Equal(t, "sess-g3", res.SessionID)
	require.False(t, res.AgentReplied)
	require.Equal(t, "gemini status=error", res.Error)
}

func TestParseByName_Gemini(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"init","session_id":"sess-g4"}`,
		`{"type":"message","role":"assistant","content":"hi","delta":true}`,
		`{"type":"result","status":"success"}`,
	}, "\n")

	res := &Result{}
	err := ParseByName("gemini", strings.NewReader(stream), res, Callbacks{})
	require.NoError(t, err)
	require.Equal(t, "sess-g4", res.SessionID)
	require.True(t, res.PromptDelivered)
	require.True(t, res.AgentReplied)
	require.Equal(t, "hi", res.Reply)
}
