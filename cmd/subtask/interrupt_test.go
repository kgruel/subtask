package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"
)

func TestInterrupt_NotRunning(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	envTask := "fix/not-running"
	require.NoError(t, (&task.Task{
		Name:        envTask,
		Title:       "Not working",
		BaseBranch:  "main",
		Description: "desc",
		Schema:      1,
	}).Save())

	err := (&InterruptCmd{Task: envTask}).Run()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not working")
}

func TestInterrupt_AppendsHistoryAndSignals(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	taskName := "fix/running"
	require.NoError(t, (&task.Task{
		Name:        taskName,
		Title:       "Working",
		BaseBranch:  "main",
		Description: "desc",
		Schema:      1,
	}).Save())

	runIDData, _ := json.Marshal(map[string]any{"run_id": "run123"})
	require.NoError(t, history.WriteAll(taskName, []history.Event{
		{TS: time.Now().UTC(), Type: "worker.started", Data: runIDData},
	}))

	pgid := 12345
	require.NoError(t, (&task.State{
		SupervisorPID:  os.Getpid(),
		SupervisorPGID: pgid,
		StartedAt:      time.Now().UTC(),
	}).Save(taskName))

	var gotPID, gotPGID int
	orig := interruptSignalFn
	interruptSignalFn = func(pid, pgid int) error {
		gotPID, gotPGID = pid, pgid
		return nil
	}
	t.Cleanup(func() { interruptSignalFn = orig })

	require.NoError(t, (&InterruptCmd{Task: taskName}).Run())
	require.Equal(t, os.Getpid(), gotPID)
	require.Equal(t, pgid, gotPGID)

	events, err := history.Read(taskName, history.ReadOptions{})
	require.NoError(t, err)
	require.NotEmpty(t, events)
	last := events[len(events)-1]
	require.Equal(t, "worker.interrupt", last.Type)

	var data map[string]any
	require.NoError(t, json.Unmarshal(last.Data, &data))
	require.Equal(t, "requested", data["action"])
	require.Equal(t, "SIGINT", data["signal"])
	require.Equal(t, "run123", data["run_id"])
	require.Equal(t, float64(os.Getpid()), data["supervisor_pid"])
	require.Equal(t, float64(pgid), data["supervisor_pgid"])
}

func TestInterrupt_StaleSupervisorClearsState(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	taskName := "fix/stale"
	require.NoError(t, (&task.Task{
		Name:        taskName,
		Title:       "Stale",
		BaseBranch:  "main",
		Description: "desc",
		Schema:      1,
	}).Save())

	const definitelyDeadPID = 2147483647
	require.NoError(t, (&task.State{
		SupervisorPID:  definitelyDeadPID,
		SupervisorPGID: 999999,
		StartedAt:      time.Now().UTC(),
	}).Save(taskName))

	err := (&InterruptCmd{Task: taskName}).Run()
	require.Error(t, err)
	require.Contains(t, err.Error(), "stale")

	st, err := task.LoadState(taskName)
	require.NoError(t, err)
	require.NotNil(t, st)
	require.Zero(t, st.SupervisorPID)
	require.Zero(t, st.SupervisorPGID)
}
