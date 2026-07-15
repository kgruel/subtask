package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

// TestResolveAskContext_TaskWithStateSession: unchanged fast path — state.json
// has a SessionID, resolveAskContext returns it directly without touching history.
func TestResolveAskContext_TaskWithStateSession(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "ask/state-session"
	env.CreateTask(taskName, "Test task", "main", "desc")
	env.CreateTaskState(taskName, &task.State{SessionID: "sess-from-state", Adapter: "claude"})

	sid, name, err := resolveAskContext(taskName, "claude")
	require.NoError(t, err)
	assert.Equal(t, "sess-from-state", sid)
	assert.Empty(t, name)
}

// TestResolveAskContext_TaskWithHistoryOnlySession: state.json is gone (as it
// would be after merge/close, or when a task folder was synced to another
// machine without internal/), but history.jsonl still records the last
// worker.session event — resolveAskContext should recover the session from it
// instead of falling through to raw-session-ID (which would pass the task name
// itself as a session ID).
func TestResolveAskContext_TaskWithHistoryOnlySession(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "ask/history-session"
	env.CreateTask(taskName, "Test task", "main", "desc")
	env.CreateTaskHistory(taskName, append(mustHistoryOpen(t, "main"),
		history.Event{Type: "worker.session", Data: mustJSON(map[string]any{
			"action": "follow_up", "harness": "claude", "session_id": "sess-from-history",
		})}))
	// No state.json at all — simulates merge/close cleanup or a synced task folder.

	sid, name, err := resolveAskContext(taskName, "claude")
	require.NoError(t, err)
	assert.Equal(t, "sess-from-history", sid)
	assert.Empty(t, name)
}

// TestResolveAskContext_TaskWithHistoryOnlySession_AdapterMismatch verifies the
// recovered session is not silently resumed across adapters.
func TestResolveAskContext_TaskWithHistoryOnlySession_AdapterMismatch(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "ask/history-session-mismatch"
	env.CreateTask(taskName, "Test task", "main", "desc")
	env.CreateTaskHistory(taskName, append(mustHistoryOpen(t, "main"),
		history.Event{Type: "worker.session", Data: mustJSON(map[string]any{
			"action": "follow_up", "harness": "codex", "session_id": "sess-from-history",
		})}))

	_, _, err := resolveAskContext(taskName, "claude")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "codex")
	assert.Contains(t, err.Error(), "claude")
}

// TestResolveAskContext_TaskWithNoSession: a real task with neither state.json
// nor any worker.session history event (never dispatched) must return a clear,
// actionable error rather than passing the task name onward as a raw session ID.
func TestResolveAskContext_TaskWithNoSession(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "ask/no-session"
	env.CreateTask(taskName, "Test task", "main", "desc")
	env.CreateTaskHistory(taskName, mustHistoryOpen(t, "main"))

	_, _, err := resolveAskContext(taskName, "claude")
	require.Error(t, err)
	assert.Contains(t, err.Error(), taskName)
	assert.Contains(t, err.Error(), "no session")
}

// TestResolveAskContext_Petname verifies petname resolution is unaffected by the
// task-history fallback (no task exists with that name).
func TestResolveAskContext_Petname(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	dir := ConversationsDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "happy-otter.uuid"), []byte("sess-petname\n"), 0o644))

	sid, name, err := resolveAskContext("happy-otter", "claude")
	require.NoError(t, err)
	assert.Equal(t, "sess-petname", sid)
	assert.Equal(t, "happy-otter", name)
}

// TestResolveAskContext_RawSessionID verifies a genuine raw session ID (no
// matching task, no matching petname) passes through unchanged.
func TestResolveAskContext_RawSessionID(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	sid, name, err := resolveAskContext("f47ac10b-58cc-4372-a567-0e02b2c3d479", "claude")
	require.NoError(t, err)
	assert.Equal(t, "f47ac10b-58cc-4372-a567-0e02b2c3d479", sid)
	assert.Empty(t, name)
}
