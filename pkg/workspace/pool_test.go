package workspace_test

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workspace"
)

func TestPoolAcquire_CreatesFirstWorkspaceWhenNoneExist(t *testing.T) {
	testutil.NewTestEnv(t, 0)

	pool := workspace.NewPool()
	acq, err := pool.Acquire()
	require.NoError(t, err)
	defer acq.Release()

	require.NotNil(t, acq.Entry)
	_, err = os.Stat(acq.Entry.Path)
	require.NoError(t, err)

	workspaces, err := workspace.ListWorkspaces()
	require.NoError(t, err)
	require.Len(t, workspaces, 1)
}

func TestPoolAcquire_ReusesExistingUnoccupiedWorkspace(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	pool := workspace.NewPool()
	acq, err := pool.Acquire()
	require.NoError(t, err)
	defer acq.Release()

	require.Equal(t, env.Workspaces[0], acq.Entry.Path)

	workspaces, err := workspace.ListWorkspaces()
	require.NoError(t, err)
	require.Len(t, workspaces, 1)
}

func TestPoolAcquire_CreatesNewWorkspaceWhenAllOccupiedAndUnderMax(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	cfg := env.Config()
	cfg.MaxWorkspaces = 2
	require.NoError(t, cfg.Save())

	env.CreateTask("busy/one", "Busy", "main", "busy")
	env.CreateTaskState("busy/one", &task.State{
		Workspace:     env.Workspaces[0],
		SupervisorPID: os.Getpid(),
		StartedAt:     time.Now(),
	})

	pool := workspace.NewPool()
	acq, err := pool.Acquire()
	require.NoError(t, err)
	defer acq.Release()

	require.NotNil(t, acq.Entry)
	require.NotEqual(t, env.Workspaces[0], acq.Entry.Path)

	workspaces, err := workspace.ListWorkspaces()
	require.NoError(t, err)
	require.Len(t, workspaces, 2)
}

// TestForTask_DraftedNoWorkspace_ActionableMessage verifies that a task which was
// drafted but never sent (no state.json, no worker events in history) returns an
// actionable message pointing the lead to subtask send.
func TestForTask_DraftedNoWorkspace_ActionableMessage(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "draft/no-ws"
	env.CreateTask(taskName, "Draft task", "main", "Description")
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: json.RawMessage(`{"reason":"draft","base_branch":"main"}`)},
	})
	// No state.json — workspace never assigned.

	_, err := workspace.ForTask(taskName)
	require.Error(t, err)
	require.Contains(t, err.Error(), "drafted but has no workspace yet")
	require.Contains(t, err.Error(), "subtask send")
}

// TestForTask_SyncedTaskWithWorkerActivity_GenericMessage verifies that a task
// whose task folder exists and has prior worker activity — but no local state.json
// (e.g. copied from another machine) — returns the generic "no workspace" error,
// not the "drafted" actionable message.
func TestForTask_SyncedTaskWithWorkerActivity_GenericMessage(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "synced/with-activity"
	env.CreateTask(taskName, "Synced task", "main", "Description")
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: json.RawMessage(`{"reason":"draft","base_branch":"main"}`)},
		{Type: "worker.started", Data: json.RawMessage(`{"run_id":"abc123"}`)},
		{Type: "worker.finished", Data: json.RawMessage(`{"run_id":"abc123","outcome":"replied","duration_ms":1000,"tool_calls":5}`)},
	})
	// No state.json — simulates a synced task folder without local runtime state.

	_, err := workspace.ForTask(taskName)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "drafted but has no workspace yet")
	require.Contains(t, err.Error(), "no workspace")
}

// TestForTask_RunningWorkerNoState_GenericMessage verifies that a task with a
// worker.started event but no matching worker.finished (worker still in flight or
// crashed) and no local state.json falls through to the generic error, not the
// "drafted" message. This exercises the RunningSince.IsZero() predicate.
func TestForTask_RunningWorkerNoState_GenericMessage(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "running/no-state"
	env.CreateTask(taskName, "Running task", "main", "Description")
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: json.RawMessage(`{"reason":"draft","base_branch":"main"}`)},
		{Type: "worker.started", Data: json.RawMessage(`{"run_id":"xyz789"}`)},
		// No worker.finished — worker still in flight or crashed.
	})
	// No state.json.

	_, err := workspace.ForTask(taskName)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "drafted but has no workspace yet")
	require.Contains(t, err.Error(), "no workspace")
}

func TestPoolAcquire_ErrorsWhenAllOccupiedAtMax(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	cfg := env.Config()
	cfg.MaxWorkspaces = 1
	require.NoError(t, cfg.Save())

	env.CreateTask("busy/one", "Busy", "main", "busy")
	env.CreateTaskState("busy/one", &task.State{
		Workspace:     env.Workspaces[0],
		SupervisorPID: os.Getpid(),
		StartedAt:     time.Now(),
	})

	pool := workspace.NewPool()
	_, err := pool.Acquire()
	require.Error(t, err)
	require.Contains(t, err.Error(), "all workspaces occupied")

	workspaces, err := workspace.ListWorkspaces()
	require.NoError(t, err)
	require.Len(t, workspaces, 1)
}
