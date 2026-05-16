package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestQuickstart_EmptyProjectShowsFirstTaskFlow(t *testing.T) {
	testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	stdout, _, err := captureStdoutStderr(t, (&QuickstartCmd{}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Welcome to subtask")
	require.Contains(t, stdout, "subtask draft fix/example")
	require.Contains(t, stdout, "subtask send fix/example")
}

func TestQuickstart_WithOpenTasksShowsEntryPoints(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	env.CreateTask("fix/open", "Open task", "main", "Do it")
	env.CreateTaskHistory("fix/open", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 1, "tool_calls": 0, "outcome": "replied"})},
	})

	stdout, _, err := captureStdoutStderr(t, (&QuickstartCmd{}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Project has 1 open task(s) (1 total")
	require.Contains(t, stdout, "1 with unread replies")
	require.Contains(t, stdout, "subtask list")
	require.Contains(t, stdout, "subtask next <task>")
}

func TestQuickstart_ClosedOnlyUsesAllCount(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	env.CreateTask("fix/closed", "Closed task", "main", "Done")
	env.CreateTaskHistory("fix/closed", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{Type: "task.closed", Data: mustJSON(map[string]any{"reason": "close"})},
	})

	stdout, _, err := captureStdoutStderr(t, (&QuickstartCmd{}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Project has 0 open task(s) (1 total")
	require.Contains(t, stdout, "subtask list -a")
}

func TestQuickstart_FirstForcesFirstTaskFlow(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	env.CreateTask("fix/open", "Open task", "main", "Do it")
	env.CreateTaskHistory("fix/open", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
	})

	stdout, _, err := captureStdoutStderr(t, (&QuickstartCmd{First: true}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Welcome to subtask")
	require.NotContains(t, stdout, "Project has")
}

func TestQuickstart_OutsideProjectShowsRecoveryHint(t *testing.T) {
	orig, err := os.Getwd()
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })

	_, _, err = captureStdoutStderr(t, (&QuickstartCmd{}).Run)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not in a subtask-initialized project")
	require.Contains(t, err.Error(), "subtask install")
}
