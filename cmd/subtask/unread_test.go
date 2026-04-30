package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestUnread_WorkerRepliedNoFollowUp_ReportsUnread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "fix/unread"
	env.CreateTask(taskName, "Worker replied", "main", "")

	mock := harness.NewMockHarness().WithResult("Worker reply text", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.True(t, unread, "worker replied with no lead follow-up should be unread")
}

func TestUnread_LeadFollowedUp_ReportsRead(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "fix/followed"
	env.CreateTask(taskName, "Worker replied, lead followed up", "main", "")

	mock := harness.NewMockHarness().WithResult("First reply", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	// Lead replies before checking — second send appends a lead message after worker.finished.
	mock2 := harness.NewMockHarness().WithResult("Second reply", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Also handle X"}).WithHarness(mock2).Run())

	// Now the lead message for "Also handle X" was appended, then worker.finished for the second
	// reply. So state is "unread" again — verify that's what we see.
	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.True(t, unread, "second worker reply with no follow-up should be unread")

	// Manually append a lead message to simulate the lead engaging without sending.
	require.NoError(t, history.Append(taskName, history.Event{
		Type:    "message",
		Role:    "lead",
		Content: "Looks good",
	}))

	unread, err = taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread, "lead message after worker.finished should mark task as read")
}

func TestUnread_FreshTask_NotUnread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "fix/fresh"
	env.CreateTask(taskName, "Drafted, never sent", "main", "")

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread, "drafted-only task with no worker.finished should not be unread")
}
