package harness

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseCodexExecJSONL_TooLongLineDoesNotPreventLaterEvents(t *testing.T) {
	tooLong := strings.Repeat("x", 100) + "\n"
	thread := `{"type":"thread.started","thread_id":"sess-1"}` + "\n"

	result := &Result{}
	err := parseCodexExecJSONL(strings.NewReader(tooLong+thread), result, Callbacks{}, 50)
	require.Error(t, err)
	require.Equal(t, "sess-1", result.SessionID)
	require.True(t, result.PromptDelivered)
}
