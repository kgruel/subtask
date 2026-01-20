package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/harness"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/testutil"
)

type blockingHarness struct {
	SessionID string
	Started   chan struct{}
	Release   chan struct{}
}

func (h *blockingHarness) Run(ctx context.Context, cwd, prompt, continueFrom string, cb harness.Callbacks) (*harness.Result, error) {
	if cb.OnSessionStart != nil && h.SessionID != "" {
		cb.OnSessionStart(h.SessionID)
	}
	if h.Started != nil {
		close(h.Started)
	}

	select {
	case <-h.Release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return &harness.Result{
		Reply:           "ok",
		SessionID:       h.SessionID,
		PromptDelivered: true,
		AgentReplied:    true,
	}, nil
}

func (h *blockingHarness) Review(cwd string, target harness.ReviewTarget, instructions string) (string, error) {
	return "", nil
}
func (h *blockingHarness) MigrateSession(sessionID, oldCwd, newCwd string) error {
	return nil
}
func (h *blockingHarness) DuplicateSession(sessionID, oldCwd, newCwd string) (string, error) {
	return "", nil
}

func TestLogsCmd_WorksWhileTaskWorking_AfterSessionStart(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	env := testutil.NewTestEnv(t, 1)

	taskName := "fix/logs-working"
	env.CreateTask(taskName, "Logs while working", "main", "desc")
	env.CreateTaskHistory(taskName, []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "base_commit": gitCmdOutput(t, env.RootDir, "rev-parse", "HEAD")})},
		{Type: "stage.changed", Data: mustJSON(map[string]any{"from": "", "to": "implement"})},
	})

	// Create a session file so LogsCmd can resolve it once SessionID is persisted.
	sessionID := "sess-123"
	sessionsDir := filepath.Join(tmpHome, ".codex", "sessions")
	require.NoError(t, os.MkdirAll(sessionsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(sessionsDir, "test-"+sessionID+".jsonl"), nil, 0o644))

	h := &blockingHarness{
		SessionID: sessionID,
		Started:   make(chan struct{}),
		Release:   make(chan struct{}),
	}

	sendDone := make(chan error, 1)
	go func() {
		sendDone <- (&SendCmd{Task: taskName, Prompt: "do work"}).WithHarness(h).Run()
	}()

	select {
	case <-h.Started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for session start")
	}

	st, err := task.LoadState(taskName)
	require.NoError(t, err)
	require.NotNil(t, st)
	require.NotZero(t, st.SupervisorPID)
	require.Equal(t, sessionID, st.SessionID)

	require.NoError(t, (&LogsCmd{TaskOrSession: taskName}).Run())

	close(h.Release)
	require.NoError(t, <-sendDone)
}
