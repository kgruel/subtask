package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"
	"github.com/zippoxer/subtask/pkg/workspace"
)

func setProjectHarness(t *testing.T, harnessName string, opts map[string]any) {
	t.Helper()
	cfg, err := workspace.LoadConfig()
	require.NoError(t, err)
	cfg.Harness = harnessName
	if opts != nil {
		cfg.Options = opts
	}
	require.NoError(t, cfg.Save())
}

func TestSend_ReasoningWithClaudeErrors(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	taskName := "send/claude-reasoning"
	env.CreateTask(taskName, "Test task", "main", "Description")
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")})},
	})

	setProjectHarness(t, "claude", map[string]any{"model": "claude-opus-4-5-20251101"})

	_, _, err := captureStdoutStderr(t, (&SendCmd{
		Task:      taskName,
		Prompt:    "Hello",
		Reasoning: "high",
	}).Run)
	require.Error(t, err)
	require.Contains(t, err.Error(), "reasoning is codex-only")
}

func TestAsk_ReasoningWithClaudeErrors(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	setProjectHarness(t, "claude", map[string]any{"model": "claude-opus-4-5-20251101"})

	_, _, err := captureStdoutStderr(t, (&AskCmd{
		Prompt:    "Hello",
		Reasoning: "high",
	}).Run)
	require.Error(t, err)
	require.Contains(t, err.Error(), "reasoning is codex-only")
}

func TestSend_HarnessMismatchErrors(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	taskName := "send/harness-mismatch"
	env.CreateTask(taskName, "Test task", "main", "Description")
	env.CreateTaskState(taskName, &task.State{
		SessionID: "session-1",
		Harness:   "codex",
	})
	env.CreateTaskHistory(taskName, mustHistoryOpen(t, "main"))

	setProjectHarness(t, "claude", map[string]any{"model": "claude-opus-4-5-20251101"})

	_, _, err := captureStdoutStderr(t, (&SendCmd{Task: taskName, Prompt: "Hello"}).Run)
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "sessions are not compatible")
}

func TestSend_HarnessMismatchBackfillsFromHistory(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	taskName := "send/harness-mismatch-backfill"
	env.CreateTask(taskName, "Test task", "main", "Description")
	env.CreateTaskState(taskName, &task.State{
		SessionID: "session-1",
	})
	env.CreateTaskHistory(taskName, append(
		mustHistoryOpen(t, "main"),
		history.Event{
			Type: "worker.session",
			Data: mustJSON(map[string]any{
				"action":     "started",
				"harness":    "codex",
				"session_id": "session-1",
			}),
		},
	))

	setProjectHarness(t, "claude", map[string]any{"model": "claude-opus-4-5-20251101"})

	_, _, err := captureStdoutStderr(t, (&SendCmd{Task: taskName, Prompt: "Hello"}).Run)
	require.Error(t, err)

	st, err := task.LoadState(taskName)
	require.NoError(t, err)
	require.Equal(t, "codex", st.Harness)
}

func TestShow_ModelUsesTaskOverride(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	setProjectHarness(t, "mock", map[string]any{"model": "config-model"})

	taskName := "show/model-override"
	env.CreateTask(taskName, "Test task", "main", "Description")
	loaded, err := task.Load(taskName)
	require.NoError(t, err)
	loaded.Model = "task-model"
	require.NoError(t, loaded.Save())
	env.CreateTaskHistory(taskName, mustHistoryOpen(t, "main"))

	stdout, _, err := captureStdoutStderr(t, (&ShowCmd{Task: taskName}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Model: task-model")
}

func TestShow_ModelIncludesReasoningWhenCodex(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	setProjectHarness(t, "codex", map[string]any{
		"model":     "gpt-5.2",
		"reasoning": "high",
	})

	taskName := "show/model-reasoning"
	env.CreateTask(taskName, "Test task", "main", "Description")
	env.CreateTaskHistory(taskName, mustHistoryOpen(t, "main"))

	stdout, _, err := captureStdoutStderr(t, (&ShowCmd{Task: taskName}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Model: gpt-5.2 (high)")
}
