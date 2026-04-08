package workspace_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
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
