package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/harness"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", message)
}

func commitEmpty(t *testing.T, dir, message string) {
	t.Helper()
	gitCmd(t, dir, "commit", "--allow-empty", "-m", message)
}

func TestGolden_List_CommitsBehind(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withFixedNow(t, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	baseCommit := gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")
	commitEmpty(t, env.RootDir, "one")
	commitEmpty(t, env.RootDir, "two")

	taskName := "list/behind"
	env.CreateTask(taskName, "Behind task", "main", "Description")
	env.CreateTaskState(taskName, &task.State{
		Workspace: "",
	})
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
	})

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			stdout, stderr, err := captureStdoutStderr(t, (&ListCmd{}).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			testutil.AssertGoldenOutput(t, "testdata/list/commits_behind", stdout)
		})
	}
}

func TestGolden_Show_Conflicts(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withFixedNow(t, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))

	// Create a base commit with stable files.
	writeFile(t, env.RootDir, "a.txt", "base\n")
	writeFile(t, env.RootDir, "b.txt", "base\n")
	commitAll(t, env.RootDir, "base files")
	baseCommit := gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")

	// Worker workspace based on baseCommit with committed changes.
	gitCmd(t, env.RootDir, "worktree", "add", "--detach", "ws1", baseCommit)
	writeFile(t, "ws1", "a.txt", "worker\n")
	writeFile(t, "ws1", "b.txt", "worker\n")
	commitAll(t, "ws1", "worker changes")

	// Advance main (making the task behind) and touch the same files (conflicts).
	writeFile(t, env.RootDir, "a.txt", "main-1\n")
	commitAll(t, env.RootDir, "main change a")
	writeFile(t, env.RootDir, "b.txt", "main-2\n")
	commitAll(t, env.RootDir, "main change b")

	taskName := "show/behind-conflicts"
	env.CreateTask(taskName, "Conflict task", "main", "Description")
	env.CreateTaskState(taskName, &task.State{
		Workspace: "ws1",
		StartedAt: time.Now().Add(-time.Hour),
	})
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 1234, "tool_calls": 1, "outcome": "replied"})},
	})

	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			withOutputMode(t, pretty)

			stdout, stderr, err := captureStdoutStderr(t, (&ShowCmd{Task: taskName}).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			testutil.AssertGoldenOutput(t, "testdata/show/behind_conflicts", stdout)
		})
	}
}

func TestGolden_Send_PrintsConflicts(t *testing.T) {
	for _, pretty := range []bool{false, true} {
		t.Run(modeName(pretty), func(t *testing.T) {
			env := testutil.NewTestEnv(t, 0)
			withFixedNow(t, time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
			withOutputMode(t, pretty)

			// Base commit.
			writeFile(t, env.RootDir, "a.txt", "base\n")
			writeFile(t, env.RootDir, "b.txt", "base\n")
			commitAll(t, env.RootDir, "base files")
			baseCommit := gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")

			// Existing workspace with committed worker changes.
			gitCmd(t, env.RootDir, "worktree", "add", "--detach", "ws1", baseCommit)
			writeFile(t, "ws1", "a.txt", "worker\n")
			writeFile(t, "ws1", "b.txt", "worker\n")
			commitAll(t, "ws1", "worker changes")

			// Advance main + overlap.
			writeFile(t, env.RootDir, "a.txt", "main-1\n")
			commitAll(t, env.RootDir, "main change a")
			writeFile(t, env.RootDir, "b.txt", "main-2\n")
			commitAll(t, env.RootDir, "main change b")

			taskName := "send/behind-conflicts"
			env.CreateTask(taskName, "Send staleness output", "main", "Description")
			env.CreateTaskState(taskName, &task.State{
				Workspace: "ws1",
			})
			env.CreateTaskHistory(taskName, []history.Event{
				{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": baseCommit})},
				{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
			})

			mock := harness.NewMockHarness().
				WithResult("Done", "session-1").
				WithToolCalls(3)

			stdout, stderr, err := captureStdoutStderr(t, (&SendCmd{Task: taskName, Prompt: "Do it"}).WithHarness(mock).Run)
			require.NoError(t, err)
			require.Empty(t, stderr)
			testutil.AssertGoldenOutput(t, "testdata/send/behind_conflicts", stdout)
		})
	}
}

func TestIntegration_BaseCommitTracking_DraftThenMainAdvances(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	taskName := "integration/base-commit"
	_, _, err := captureStdoutStderr(t, (&DraftCmd{
		Task:        taskName,
		Description: "Test description",
		Base:        "main",
		Title:       "Integration test",
	}).Run)
	require.NoError(t, err)

	tail, err := history.Tail(taskName)
	require.NoError(t, err)
	require.NotEmpty(t, tail.BaseCommit)

	commitEmpty(t, env.RootDir, "one")
	commitEmpty(t, env.RootDir, "two")

	stdout, _, err := captureStdoutStderr(t, (&ListCmd{}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "(2 behind)")

	stdout, _, err = captureStdoutStderr(t, (&ShowCmd{Task: taskName}).Run)
	require.NoError(t, err)
}
