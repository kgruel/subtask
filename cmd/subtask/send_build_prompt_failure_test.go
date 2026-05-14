package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

// TestSendCmd_BuildPromptFailureLeavesTaskNonRunning is the regression
// test for the failure mode where BuildPrompt errors (e.g. a missing
// agent YAML) bypassed the worker-failure cleanup, leaving the task
// stuck appearing "running" until stale cleanup.
//
// prepareWorkspaceAndState claims SupervisorPID and appends
// worker.started before BuildPrompt runs. So a bare `return err` from
// BuildPrompt would leave SupervisorPID set and no terminal event in
// history. The fix routes the error through the existing runErr cleanup
// path; this test pins that behavior.
func TestSendCmd_BuildPromptFailureLeavesTaskNonRunning(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	taskName := "fix/build-prompt-fail"
	env.CreateTask(taskName, "Build prompt fail", "main", "desc")
	env.CreateTaskHistory(taskName, mustHistoryOpen(t, "main"))

	// Point the task at an agent that does not exist on disk. BuildPrompt
	// re-resolves agents every call; a missing file is exactly the
	// failure mode P1 #1 cares about.
	tk, err := task.Load(taskName)
	require.NoError(t, err)
	tk.Agent = "ghost"
	require.NoError(t, tk.Save())

	// Sanity: no agents/ directory or its file.
	_, statErr := os.Stat(filepath.Join(env.RootDir, ".subtask", "agents", "ghost.yaml"))
	require.True(t, os.IsNotExist(statErr), "agent file must not exist for this test")

	mock := harness.NewMockHarness()

	_, _, err = captureStdoutStderr(t, (&SendCmd{Task: taskName, Prompt: "Do it"}).WithHarness(mock).Run)
	require.Error(t, err, "send must surface the BuildPrompt error")

	// Task state: SupervisorPID cleared, LastError set.
	st, err := task.LoadState(taskName)
	require.NoError(t, err)
	require.Zero(t, st.SupervisorPID, "SupervisorPID must be cleared after BuildPrompt failure")
	require.NotEmpty(t, st.LastError, "LastError must record the failure")

	// History: worker.finished with outcome=error and an error_message.
	events, err := history.Read(taskName, history.ReadOptions{})
	require.NoError(t, err)

	var sawFinishedError bool
	for _, ev := range events {
		if ev.Type != "worker.finished" {
			continue
		}
		var data map[string]any
		require.NoError(t, json.Unmarshal(ev.Data, &data))
		if data["outcome"] == "error" {
			sawFinishedError = true
			require.NotEmpty(t, data["error_message"], "error_message must be set on the finished event")
		}
	}
	require.True(t, sawFinishedError, "expected worker.finished with outcome=error")
}
