package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/harness"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func TestDiffCmd_OpenTask_IncludesUncommittedChanges(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "diff/open"
	env.CreateTask(taskName, "Open diff", "main", "Description")

	mock := harness.NewMockHarness().WithResult("Done", "session-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Do it"}).WithHarness(mock).Run())

	state, err := task.LoadState(taskName)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotEmpty(t, state.Workspace)

	writeFile(t, state.Workspace, "README.md", "# Test Repo\nopen-change\n")

	stdout, stderr, err := captureStdoutStderr(t, (&DiffCmd{Task: taskName}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)
	assert.Contains(t, stdout, "diff --git a/README.md b/README.md")
	assert.Contains(t, stdout, "+open-change")
}

func TestDiffCmd_ClosedTask_UsesRepoBranchDiff(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "diff/closed"
	env.CreateTask(taskName, "Closed diff", "main", "Description")

	mock := harness.NewMockHarness().WithResult("Done", "session-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Do it"}).WithHarness(mock).Run())

	state, err := task.LoadState(taskName)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotEmpty(t, state.Workspace)

	writeFile(t, state.Workspace, "README.md", "# Test Repo\nclosed-change\n")
	commitAll(t, state.Workspace, "closed change")

	// Simulate a "closed" task by clearing the workspace and writing a close event.
	state.Workspace = ""
	state.SupervisorPID = 0
	state.StartedAt = time.Time{}
	require.NoError(t, state.Save(taskName))
	_ = history.Append(taskName, history.Event{Type: "task.closed", Data: mustJSON(map[string]any{"reason": "close"})})

	stdout, stderr, err := captureStdoutStderr(t, (&DiffCmd{Task: taskName}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)
	assert.Contains(t, stdout, "diff --git a/README.md b/README.md")
	assert.Contains(t, stdout, "+closed-change")
}

func TestDiffCmd_MergedTask_BranchDeletedShowsSquashCommit(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "diff/merged"
	env.CreateTask(taskName, "Merged diff", "main", "Description")

	mock := harness.NewMockHarness().WithResult("Done", "session-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Do it"}).WithHarness(mock).Run())

	state, err := task.LoadState(taskName)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotEmpty(t, state.Workspace)

	writeFile(t, state.Workspace, "README.md", "# Test Repo\nmerged-change\n")
	commitAll(t, state.Workspace, "merged change")

	// Create a squash merge commit on main and delete the task branch.
	gitCmd(t, env.RootDir, "checkout", "main")
	gitCmd(t, env.RootDir, "merge", "--squash", taskName)
	gitCmd(t, env.RootDir, "commit", "-m", "squash merge")
	mergedCommit := gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")
	// Detach the workspace so the branch can be deleted from the main worktree.
	gitCmd(t, state.Workspace, "checkout", "--detach", "HEAD")
	gitCmd(t, env.RootDir, "branch", "-D", taskName)

	_ = history.Append(taskName, history.Event{Type: "task.merged", Data: mustJSON(map[string]any{"commit": mergedCommit, "into": "main"})})

	stdout, stderr, err := captureStdoutStderr(t, (&DiffCmd{Task: taskName}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)
	assert.Contains(t, stdout, "diff --git a/README.md b/README.md")
	assert.Contains(t, stdout, "+merged-change")
}

func TestDiffCmd_DraftTask_NoBranchErrorsWithStartHint(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	taskName := "diff/draft"
	_, _, err := captureStdoutStderr(t, (&DraftCmd{
		Task:        taskName,
		Description: "Description",
		Base:        "main",
		Title:       "Draft diff",
	}).Run)
	require.NoError(t, err)

	_, _, err = captureStdoutStderr(t, (&DiffCmd{Task: taskName}).Run)
	require.Error(t, err)
	assert.ErrorContains(t, err, "hasn't started")
	assert.ErrorContains(t, err, "subtask send")
}
