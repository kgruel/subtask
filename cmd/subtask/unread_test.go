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

func TestUnread_SilentStage_NotUnread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	installCustomWorkflow(t, env, "silent-flow", `name: silent-flow
description: Workflow with a silent commit stage
stages:
  - name: implement
    instructions: Do work.
  - name: commit
    notify: false
    instructions: Commit it.
  - name: ready
    instructions: Done.
`)

	taskName := "fix/silent"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Silent stage test",
		Description: "Testing notify:false",
		Base:        "main",
		Workflow:    "silent-flow",
	}).Run())

	mock := harness.NewMockHarness().WithResult("ok", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	// Default first stage ("implement") is not silent — reply should be unread.
	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.True(t, unread, "task in non-silent stage should be unread")

	// Move to the silent stage. Use --no-send: commit stage has no
	// worker_instructions, but we want to be explicit about not dispatching.
	require.NoError(t, (&StageCmd{Task: taskName, Stage: "commit", NoSend: true}).Run())

	unread, err = taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread, "task in stage with notify:false should be silenced")
}

// Regression: a closed task whose folder still resides on disk must not
// surface in the unread view. task.List() returns disk-resident folders,
// so without index-aware filtering, closed tasks show as phantom unread.
func TestUnread_ClosedTaskNotSurfaced(t *testing.T) {
	env := testutil.NewTestEnv(t, 2)
	withOutputMode(t, false)

	// Open task with a worker reply (legitimately unread).
	openName := "fix/open"
	env.CreateTask(openName, "Open task with reply", "main", "")
	mockOpen := harness.NewMockHarness().WithResult("open reply", "sess-open")
	require.NoError(t, (&SendCmd{Task: openName, Prompt: "Go"}).WithHarness(mockOpen).Run())

	// Closed task that previously had a worker reply. The folder remains on
	// disk but the index should mark it closed and the unread view should skip it.
	closedName := "fix/closed-but-onfs"
	env.CreateTask(closedName, "Closed task with stale reply", "main", "")
	mockClosed := harness.NewMockHarness().WithResult("stale reply", "sess-closed")
	require.NoError(t, (&SendCmd{Task: closedName, Prompt: "Go"}).WithHarness(mockClosed).Run())
	require.NoError(t, (&CloseCmd{Task: closedName, Abandon: true}).Run())

	names, err := openTaskNames()
	require.NoError(t, err)
	assert.Contains(t, names, openName, "open task with reply must be in openTaskNames")
	assert.NotContains(t, names, closedName, "closed task must not appear in openTaskNames even if folder remains")
}

func TestUnread_FreshTask_NotUnread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "fix/fresh"
	env.CreateTask(taskName, "Drafted, never sent", "main", "")

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread, "drafted-only task with no worker.finished should not be unread")
}
