package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

// First send re-resolves the task's base branch to its current local HEAD,
// so a task drafted before main advanced still picks up the new commits.
func TestIntegration_SendReresolvesBaseOnFirstRun(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	taskName := "integration/fresh-base-on-run"
	_, _, err := captureStdoutStderr(t, (&DraftCmd{
		Task:        taskName,
		Description: "Test description",
		Base:        "main",
		Title:       "Integration test",
	}).Run)
	require.NoError(t, err)

	tail, err := history.Tail(taskName)
	require.NoError(t, err)
	draftBaseCommit := tail.BaseCommit
	require.NotEmpty(t, draftBaseCommit)

	commitEmpty(t, env.RootDir, "advance main")
	currentMainCommit := gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")
	require.NotEqual(t, draftBaseCommit, currentMainCommit)

	mock := harness.NewMockHarness().WithResult("Done", "session-1")
	_, _, err = captureStdoutStderr(t, (&SendCmd{Task: taskName, Prompt: "Do it"}).WithHarness(mock).Run)
	require.NoError(t, err)

	state, err := task.LoadState(taskName)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotEmpty(t, state.Workspace)
	workspaceHead := gitCmdOutput(t, state.Workspace, "rev-parse", "HEAD")
	require.Equal(t, currentMainCommit, workspaceHead, "first send should branch from current base-branch HEAD, not the draft-time commit")

	// History should reflect the actual base commit used so downstream diff/staleness is accurate.
	tailAfter, err := history.Tail(taskName)
	require.NoError(t, err)
	require.Equal(t, currentMainCommit, tailAfter.BaseCommit)
}

// --pinned-base opts back into the legacy "branch from draft-time commit" behavior.
func TestIntegration_SendPinnedBaseUsesDraftCommit(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	taskName := "integration/pinned-base-on-run"
	_, _, err := captureStdoutStderr(t, (&DraftCmd{
		Task:        taskName,
		Description: "Test description",
		Base:        "main",
		Title:       "Integration test",
	}).Run)
	require.NoError(t, err)

	tail, err := history.Tail(taskName)
	require.NoError(t, err)
	draftBaseCommit := tail.BaseCommit
	require.NotEmpty(t, draftBaseCommit)

	commitEmpty(t, env.RootDir, "advance main")
	currentMainCommit := gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")
	require.NotEqual(t, draftBaseCommit, currentMainCommit)

	mock := harness.NewMockHarness().WithResult("Done", "session-1")
	_, _, err = captureStdoutStderr(t, (&SendCmd{Task: taskName, Prompt: "Do it", PinnedBase: true}).WithHarness(mock).Run)
	require.NoError(t, err)

	state, err := task.LoadState(taskName)
	require.NoError(t, err)
	require.NotNil(t, state)
	workspaceHead := gitCmdOutput(t, state.Workspace, "rev-parse", "HEAD")
	require.Equal(t, draftBaseCommit, workspaceHead, "--pinned-base should branch from the draft-time captured commit")

	tailAfter, err := history.Tail(taskName)
	require.NoError(t, err)
	require.Equal(t, draftBaseCommit, tailAfter.BaseCommit)
}
