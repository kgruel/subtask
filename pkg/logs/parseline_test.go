package logs

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func collectParseLine(p Parser, line string) []LogEntry {
	var out []LogEntry
	p.ParseLine([]byte(line), func(e LogEntry) { out = append(out, e) })
	return out
}

// TestClaudeParser_ParseLine_EmitsAllParts pins the fix for the shadow parser's
// first-match-wins bug: a single assistant line carrying both a text part and a
// tool_use part must emit BOTH entries (the old follow-mode parser dropped the
// second), and a Bash tool_use must render the canonical "$ cmd" summary, not
// "→ Bash".
func TestClaudeParser_ParseLine_EmitsAllParts(t *testing.T) {
	p := &ClaudeParser{}
	line := `{"type":"assistant","timestamp":"2026-01-13T22:55:30.121Z","message":{"role":"assistant","content":[{"type":"text","text":"on it"},{"type":"tool_use","name":"Bash","input":{"command":"git status"}}]}}`

	entries := collectParseLine(p, line)
	require.Len(t, entries, 2, "both the text and tool_use parts must be emitted")
	require.Equal(t, KindAgentMessage, entries[0].Kind)
	require.Equal(t, "on it", entries[0].Summary)
	require.Equal(t, KindToolCall, entries[1].Kind)
	require.Equal(t, "$ git status", entries[1].Summary)
}

// TestClaudeParser_ParseLine_NoSessionStart guards that ParseLine never emits a
// session header even on a sessionId-bearing line — that belongs to ParseFile
// only, so follow mode doesn't reprint "Session started" on the tail.
func TestClaudeParser_ParseLine_NoSessionStart(t *testing.T) {
	p := &ClaudeParser{}
	line := `{"type":"user","timestamp":"2026-01-13T22:55:27.897Z","sessionId":"sess","cwd":"/tmp/p","version":"2.1.6","message":{"role":"user","content":"hi"}}`

	entries := collectParseLine(p, line)
	for _, e := range entries {
		require.NotEqual(t, KindSessionStart, e.Kind, "ParseLine must not emit KindSessionStart")
	}
	require.Len(t, entries, 1)
	require.Equal(t, KindUserMessage, entries[0].Kind)
	require.Equal(t, "hi", entries[0].Summary)
}

// TestCodexParser_ParseLine_ContentKinds covers the content event types follow
// mode now renders identically to file mode, including tool calls/outputs and
// errors the shadow parser never emitted.
func TestCodexParser_ParseLine_ContentKinds(t *testing.T) {
	p := &CodexParser{}

	cases := []struct {
		name    string
		line    string
		kind    EntryKind
		summary string
	}{
		{
			name:    "function_call renders canonical shell summary",
			line:    `{"timestamp":"2026-01-13T22:55:30Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":\"ls -la\"}","call_id":"c1"}}`,
			kind:    KindToolCall,
			summary: "$ ls -la",
		},
		{
			name:    "function_call_output emits tool output",
			line:    `{"timestamp":"2026-01-13T22:55:31Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"hello world"}}`,
			kind:    KindToolOutput,
			summary: "hello world",
		},
		{
			name:    "event_msg agent_message",
			line:    `{"timestamp":"2026-01-13T22:55:32Z","type":"event_msg","payload":{"type":"agent_message","message":"hi there"}}`,
			kind:    KindAgentMessage,
			summary: "hi there",
		},
		{
			name:    "event_msg agent_reasoning",
			line:    `{"timestamp":"2026-01-13T22:55:33Z","type":"event_msg","payload":{"type":"agent_reasoning","text":"thinking"}}`,
			kind:    KindReasoning,
			summary: "thinking",
		},
		{
			name:    "error event",
			line:    `{"timestamp":"2026-01-13T22:55:34Z","type":"error","payload":{"type":"error","message":"boom"}}`,
			kind:    KindError,
			summary: "boom",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := collectParseLine(p, tc.line)
			require.Len(t, entries, 1)
			require.Equal(t, tc.kind, entries[0].Kind)
			require.Equal(t, tc.summary, entries[0].Summary)
		})
	}
}

// TestCodexParser_ParseLine_NoSessionStart guards that session_meta is not
// handled by ParseLine (no SessionInfo mutation, no header) — only ParseFile.
func TestCodexParser_ParseLine_NoSessionStart(t *testing.T) {
	p := &CodexParser{}
	line := `{"timestamp":"2026-01-13T22:55:27Z","type":"session_meta","payload":{"id":"s1","cwd":"/tmp","cli_version":"1.0"}}`

	entries := collectParseLine(p, line)
	require.Empty(t, entries, "ParseLine must not emit anything for session_meta")
}
