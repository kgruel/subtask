package harness

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseOpenCodeStream_SessionToolAndReply(t *testing.T) {
	input := strings.Join([]string{
		`not json`,
		`{"type":"step_start","sessionID":"ses_123"}`,
		`{"type":"tool_use","sessionID":"ses_123","part":{"type":"tool_use","id":"prt_1"}}`,
		`{"type":"text","sessionID":"ses_123","part":{"type":"text","id":"prt_2","text":"Hello "}}`,
		`{"type":"text","sessionID":"ses_123","part":{"type":"text","id":"prt_3","text":"world!"}}`,
	}, "\n") + "\n"

	var started []string
	toolCalls := 0

	res := &Result{}
	err := parseOpenCodeStream(strings.NewReader(input), res, Callbacks{
		OnSessionStart: func(sessionID string) {
			started = append(started, sessionID)
		},
		OnToolCall: func(_ time.Time) {
			toolCalls++
		},
	})
	require.NoError(t, err)

	require.Equal(t, []string{"ses_123"}, started)
	require.Equal(t, 1, toolCalls)
	require.Equal(t, "ses_123", res.SessionID)
	require.True(t, res.PromptDelivered)
	require.True(t, res.AgentReplied)
	require.Equal(t, "Hello world!", res.Reply)
}
