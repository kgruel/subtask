package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestSendCmd_EmptyReplyIsErrorAndNoWorkerMessage(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	taskName := "fix/empty-reply"
	env.CreateTask(taskName, "Empty reply", "main", "desc")
	env.CreateTaskHistory(taskName, mustHistoryOpen(t, "main"))

	mock := harness.NewMockHarness()
	mock.RunResult = &harness.Result{
		Reply:           "",
		SessionID:       "sess-1",
		PromptDelivered: true,
		AgentReplied:    false,
	}

	_, _, err := captureStdoutStderr(t, (&SendCmd{Task: taskName, Prompt: "Do it"}).WithHarness(mock).Run)
	require.Error(t, err)

	events, err := history.Read(taskName, history.ReadOptions{})
	require.NoError(t, err)

	for _, ev := range events {
		if ev.Type == "message" && ev.Role == "worker" {
			t.Fatalf("unexpected worker message in history (content=%q)", ev.Content)
		}
	}

	var sawFinishedError bool
	for _, ev := range events {
		if ev.Type != "worker.finished" {
			continue
		}
		var data map[string]any
		require.NoError(t, json.Unmarshal(ev.Data, &data))
		if data["outcome"] == "error" {
			sawFinishedError = true
			require.NotEmpty(t, data["error_message"])
		}
	}
	require.True(t, sawFinishedError, "expected worker.finished with outcome=error")
}
