package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/harness"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func TestIntegration_SendUsesDraftBaseCommitOnFirstRun(t *testing.T) {
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
	require.Equal(t, draftBaseCommit, workspaceHead)
}
