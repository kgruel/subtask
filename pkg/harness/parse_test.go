package harness

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseByName_Claude(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","cwd":"/tmp/x","session_id":"sess-42"}`,
		`{"type":"assistant","session_id":"sess-42","message":{"role":"assistant","content":[{"type":"tool_use","name":"Write","input":{}}]}}`,
		`{"type":"result","subtype":"success","session_id":"sess-42","result":"done"}`,
	}, "\n")

	var gotSession string
	toolCalls := 0
	res := &Result{}

	err := ParseByName("claude", strings.NewReader(stream), res, Callbacks{
		OnSessionStart: func(sessionID string) { gotSession = sessionID },
		OnToolCall:     func(time.Time) { toolCalls++ },
	})
	require.NoError(t, err)
	require.Equal(t, "sess-42", gotSession)
	require.Equal(t, "sess-42", res.SessionID)
	require.True(t, res.PromptDelivered)
	require.True(t, res.AgentReplied)
	require.Equal(t, "done", res.Reply)
	require.Equal(t, 1, toolCalls)
}

func TestParseByName_Codex(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"t-1"}`,
		`{"type":"item.started","item":{"type":"command_execution"}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"hello"}}`,
	}, "\n")

	var gotSession string
	toolCalls := 0
	res := &Result{}

	err := ParseByName("codex", strings.NewReader(stream), res, Callbacks{
		OnSessionStart: func(sessionID string) { gotSession = sessionID },
		OnToolCall:     func(time.Time) { toolCalls++ },
	})
	require.NoError(t, err)
	require.Equal(t, "t-1", gotSession)
	require.Equal(t, "t-1", res.SessionID)
	require.True(t, res.PromptDelivered)
	require.True(t, res.AgentReplied)
	require.Equal(t, "hello", res.Reply)
	require.Equal(t, 1, toolCalls)
}

func TestParseByName_OpenCode(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"step_start","sessionID":"oc-1"}`,
		`{"type":"tool_use","sessionID":"oc-1","part":{"type":"tool_use","id":"t1"}}`,
		`{"type":"text","sessionID":"oc-1","part":{"type":"text","id":"p1","text":"hi"}}`,
	}, "\n")

	var gotSession string
	toolCalls := 0
	res := &Result{}

	err := ParseByName("opencode", strings.NewReader(stream), res, Callbacks{
		OnSessionStart: func(sessionID string) { gotSession = sessionID },
		OnToolCall:     func(time.Time) { toolCalls++ },
	})
	require.NoError(t, err)
	require.Equal(t, "oc-1", gotSession)
	require.Equal(t, "oc-1", res.SessionID)
	require.True(t, res.PromptDelivered)
	require.True(t, res.AgentReplied)
	require.Equal(t, "hi", res.Reply)
	require.Equal(t, 1, toolCalls)
}

func TestParseByName_GenericJSONL(t *testing.T) {
	stream := strings.Join([]string{
		`{"session_id":"s1","message":"first"}`,
		`{"session_id":"s1","message":"second"}`,
	}, "\n")

	res := &Result{}
	err := ParseGenericJSONL(strings.NewReader(stream), res, Callbacks{}, GenericJSONLRules{
		SessionID: ".session_id",
		Reply:     ".message",
	})
	require.NoError(t, err)
	require.Equal(t, "s1", res.SessionID)
	// Default is last-match-wins.
	require.Equal(t, "second", res.Reply)
	require.True(t, res.AgentReplied)
}

func TestParseByName_GenericJSONL_Accumulate(t *testing.T) {
	stream := strings.Join([]string{
		`{"text":"Hello "}`,
		`{"text":"world!"}`,
	}, "\n")

	res := &Result{}
	err := ParseGenericJSONL(strings.NewReader(stream), res, Callbacks{}, GenericJSONLRules{
		Reply:          ".text",
		ReplyAccumulate: true,
	})
	require.NoError(t, err)
	require.Equal(t, "Hello world!", res.Reply)
	require.True(t, res.AgentReplied)
}

func TestParseByName_Text(t *testing.T) {
	input := "line one\nline two\nline three\n"
	res := &Result{}
	err := ParseText(strings.NewReader(input), res)
	require.NoError(t, err)
	require.Equal(t, "line one\nline two\nline three\n", res.Reply)
	require.True(t, res.AgentReplied)
}

func TestParseByName_Unknown(t *testing.T) {
	res := &Result{}
	err := ParseByName("nonexistent", strings.NewReader(""), res, Callbacks{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nonexistent")
}

func TestExtractDotPath(t *testing.T) {
	tests := []struct {
		name string
		obj  map[string]any
		path string
		want string
	}{
		{
			name: "top-level string",
			obj:  map[string]any{"session_id": "abc"},
			path: ".session_id",
			want: "abc",
		},
		{
			name: "nested path",
			obj:  map[string]any{"item": map[string]any{"text": "hello"}},
			path: ".item.text",
			want: "hello",
		},
		{
			name: "missing key",
			obj:  map[string]any{"a": "b"},
			path: ".missing",
			want: "",
		},
		{
			name: "non-string leaf",
			obj:  map[string]any{"count": 42.0},
			path: ".count",
			want: "",
		},
		{
			name: "nested missing intermediate",
			obj:  map[string]any{"a": "b"},
			path: ".x.y.z",
			want: "",
		},
		{
			name: "empty path",
			obj:  map[string]any{"a": "b"},
			path: "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractDotPath(tt.obj, tt.path)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestMatchFields(t *testing.T) {
	obj := map[string]any{
		"type": "tool_use",
		"name": "Write",
	}

	require.True(t, matchFields(obj, map[string]string{"type": "tool_use"}))
	require.True(t, matchFields(obj, map[string]string{"type": "tool_use", "name": "Write"}))
	require.False(t, matchFields(obj, map[string]string{"type": "text"}))
	require.False(t, matchFields(obj, map[string]string{"missing": "key"}))
	require.True(t, matchFields(obj, map[string]string{})) // empty match = always true
}

func TestParseGenericJSONL_ToolCallDetection(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"tool_use","name":"Write"}`,
		`{"type":"text","text":"reply"}`,
		`{"type":"tool_use","name":"Read"}`,
	}, "\n")

	toolCalls := 0
	res := &Result{}
	err := ParseGenericJSONL(strings.NewReader(stream), res, Callbacks{
		OnToolCall: func(time.Time) { toolCalls++ },
	}, GenericJSONLRules{
		Reply: ".text",
		ToolCall: &ToolCallRule{
			Match: map[string]string{"type": "tool_use"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 2, toolCalls)
	require.Equal(t, "reply", res.Reply)
}

func TestParseGenericJSONL_SessionStart(t *testing.T) {
	stream := `{"session_id":"s1","type":"init"}` + "\n" +
		`{"session_id":"s1","type":"result","reply":"done"}` + "\n"

	var gotSession string
	res := &Result{}
	err := ParseGenericJSONL(strings.NewReader(stream), res, Callbacks{
		OnSessionStart: func(id string) { gotSession = id },
	}, GenericJSONLRules{
		SessionID: ".session_id",
		Reply:     ".reply",
	})
	require.NoError(t, err)
	require.Equal(t, "s1", gotSession)
	require.True(t, res.PromptDelivered)
	require.Equal(t, "done", res.Reply)
}
