package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

// seedSessionForMigration saves prior state with a session to continue and a
// workspace that no longer exists, so the next send re-acquires a different
// workspace and the migration branch (continueFrom != "" && prevWorkspace != ""
// && workspace changed) fires.
func seedSessionForMigration(t *testing.T, env *testutil.TestEnv, taskName string) {
	t.Helper()
	env.CreateTask(taskName, "Migrate session", "main", "desc")
	env.CreateTaskHistory(taskName, mustHistoryOpen(t, "main"))
	st := &task.State{
		SessionID: "sess-prev",
		Adapter:   "builtin-mock", // matches the test env adapter (no cross-harness guard)
		Workspace: filepath.Join(env.RootDir, "gone-workspace"),
	}
	require.NoError(t, st.Save(taskName))
}

// TestSendCmd_MigrateFailure_WarnsAndOmitsMigratedEvent pins that a failed
// session migration does not hard-fail the send, does not append a
// worker.session{migrated} event (history must not assert a migration that
// didn't happen), and surfaces an actionable warning.
func TestSendCmd_MigrateFailure_WarnsAndOmitsMigratedEvent(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	taskName := "fix/migrate-fail"
	seedSessionForMigration(t, env, taskName)

	mock := harness.NewMockHarness().WithMigrateError(errors.New("session file missing"))

	_, stderr, err := captureStdoutStderr(t, (&SendCmd{Task: taskName, Prompt: "Do it"}).WithHarness(mock).Run)
	require.NoError(t, err, "migrate failure must not hard-fail the send")

	require.Len(t, mock.MigrateCalls, 1, "migration should have been attempted")

	events, err := history.Read(taskName, history.ReadOptions{})
	require.NoError(t, err)
	for _, ev := range events {
		if ev.Type != "worker.session" {
			continue
		}
		var data map[string]any
		require.NoError(t, json.Unmarshal(ev.Data, &data))
		// A new-session "started" event is fine; only a "migrated" event would
		// falsely assert a migration that the failed call never performed.
		if data["action"] == "migrated" {
			t.Fatalf("unexpected worker.session{action:migrated} after migrate failure: %s", string(ev.Data))
		}
	}

	require.Contains(t, stderr, "could not migrate", "expected an actionable migration warning")
}

// TestSendCmd_MigrateSuccess_AppendsMigratedEvent pins the happy path: a
// successful migration appends the worker.session{migrated} event carrying the
// continued session id.
func TestSendCmd_MigrateSuccess_AppendsMigratedEvent(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	taskName := "fix/migrate-ok"
	seedSessionForMigration(t, env, taskName)

	mock := harness.NewMockHarness() // MigrateSession returns nil

	_, _, err := captureStdoutStderr(t, (&SendCmd{Task: taskName, Prompt: "Do it"}).WithHarness(mock).Run)
	require.NoError(t, err)

	require.Len(t, mock.MigrateCalls, 1)

	events, err := history.Read(taskName, history.ReadOptions{})
	require.NoError(t, err)
	var sawMigrated bool
	for _, ev := range events {
		if ev.Type != "worker.session" {
			continue
		}
		var data map[string]any
		require.NoError(t, json.Unmarshal(ev.Data, &data))
		if data["action"] == "migrated" {
			sawMigrated = true
			require.Equal(t, "sess-prev", data["session_id"])
		}
	}
	require.True(t, sawMigrated, "expected worker.session{action:migrated} on successful migration")
}
