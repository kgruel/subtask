package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestSendCmd_FollowUpDuplicatesSessionAndResumesFromDuplicate(t *testing.T) {
	env := testutil.NewTestEnv(t, 2)
	withOutputMode(t, false)

	baseTask := "ctx/base"
	env.CreateTask(baseTask, "Base", "main", "desc")
	env.CreateTaskHistory(baseTask, mustHistoryOpen(t, "main"))

	baseHarness := harness.NewMockHarness().WithResult("Base done", "sess-base")
	require.NoError(t, (&SendCmd{Task: baseTask, Prompt: "Do it"}).WithHarness(baseHarness).Run())

	baseState, err := task.LoadState(baseTask)
	require.NoError(t, err)
	require.NotNil(t, baseState)
	require.NotEmpty(t, baseState.Workspace)
	require.Equal(t, "sess-base", baseState.SessionID)

	followTask := "ctx/follow"
	_ = env.CreateTask(followTask, "Follow", "main", "desc")
	env.CreateTaskHistory(followTask, mustHistoryOpen(t, "main"))
	followT, err := task.Load(followTask)
	require.NoError(t, err)
	followT.FollowUp = baseTask
	require.NoError(t, followT.Save())

	followHarness := harness.NewMockHarness()
	followHarness.DuplicateResult = "sess-dup"
	followHarness.RunResult = &harness.Result{
		Reply:           "Follow done",
		SessionID:       "sess-dup",
		PromptDelivered: true,
		AgentReplied:    true,
	}

	require.NoError(t, (&SendCmd{Task: followTask, Prompt: "Continue"}).WithHarness(followHarness).Run())

	require.Len(t, followHarness.DuplicateCalls, 1)
	require.Equal(t, "sess-base", followHarness.DuplicateCalls[0].SessionID)
	require.Equal(t, baseState.Workspace, followHarness.DuplicateCalls[0].OldCWD)

	followState, err := task.LoadState(followTask)
	require.NoError(t, err)
	require.NotNil(t, followState)
	require.NotEmpty(t, followState.Workspace)
	require.Equal(t, "sess-dup", followState.SessionID)
	require.Equal(t, followState.Workspace, followHarness.DuplicateCalls[0].NewCWD)

	require.Len(t, followHarness.RunCalls, 1)
	require.Equal(t, "sess-dup", followHarness.RunCalls[0].ContinueFrom)
}
