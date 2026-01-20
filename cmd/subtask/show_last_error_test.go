package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func TestShow_IncludesLastErrorWhenStatusError(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "show/error"
	env.CreateTask(taskName, "Error task", "main", "Error description")
	env.CreateTaskState(taskName, &task.State{
		LastError: "something went wrong",
	})
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 0, "tool_calls": 0, "outcome": "error"})},
	})

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			stdout, stderr, err := captureStdoutStderr(t, (&ShowCmd{Task: taskName}).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			require.Contains(t, stdout, "something went wrong")
		})
	}
}
