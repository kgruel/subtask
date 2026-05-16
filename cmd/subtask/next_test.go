package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestNext_DraftBranches(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	env.CreateTask("fix/draft", "Draft task", "main", "Do it")
	env.CreateTaskHistory("fix/draft", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
	})

	follow := env.CreateTask("fix/follow", "Follow-up task", "main", "Continue it")
	follow.FollowUp = "fix/base"
	require.NoError(t, follow.Save())
	env.CreateTaskHistory("fix/follow", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main", "follow_up": "fix/base"})},
	})

	stdout, _, err := captureStdoutStderr(t, (&NextCmd{Task: "fix/draft"}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Task drafted but never dispatched")
	require.Contains(t, stdout, `subtask send fix/draft "..."`)

	stdout, _, err = captureStdoutStderr(t, (&NextCmd{Task: "fix/follow"}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Task drafted as a follow-up to fix/base")
	require.Contains(t, stdout, `subtask send fix/follow "Continue from the prior task and ..."`)
}

func TestNext_Working(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	env.CreateTask("fix/working", "Working task", "main", "Do it")
	env.CreateTaskState("fix/working", &task.State{SupervisorPID: os.Getpid(), StartedAt: time.Now().Add(-time.Minute)})
	env.CreateTaskHistory("fix/working", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
	})

	stdout, _, err := captureStdoutStderr(t, (&NextCmd{Task: "fix/working"}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Worker running")
	require.Contains(t, stdout, "subtask interrupt fix/working")
}

func TestNext_RepliedUnreadAndAlreadyRead(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	env.CreateTask("fix/unread", "Unread task", "main", "Do it")
	env.CreateTaskHistory("fix/unread", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 120000, "tool_calls": 0, "outcome": "replied"})},
		{Type: "message", Role: "worker", Content: "done"},
	})

	env.CreateTask("fix/read", "Read task", "main", "Do it")
	env.CreateTaskHistory("fix/read", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 120000, "tool_calls": 0, "outcome": "replied"})},
		{Type: "message", Role: "worker", Content: "done"},
		{Type: "message", Role: "lead", Content: "read"},
	})

	stdout, _, err := captureStdoutStderr(t, (&NextCmd{Task: "fix/unread"}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Unread")
	require.Contains(t, stdout, "subtask reply fix/unread")

	stdout, _, err = captureStdoutStderr(t, (&NextCmd{Task: "fix/read"}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Already read")
	require.Contains(t, stdout, `subtask send fix/read "..."`)
}

func TestNext_ErrorAndInterrupted(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	env.CreateTask("fix/interrupted", "Interrupted task", "main", "Do it")
	env.CreateTaskState("fix/interrupted", &task.State{LastError: "interrupted"})
	env.CreateTaskHistory("fix/interrupted", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 30000, "tool_calls": 0, "outcome": "error"})},
	})

	env.CreateTask("fix/error", "Error task", "main", "Do it")
	env.CreateTaskState("fix/error", &task.State{LastError: "boom"})
	env.CreateTaskHistory("fix/error", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{Type: "worker.finished", Data: mustJSON(map[string]any{"run_id": "r1", "duration_ms": 30000, "tool_calls": 0, "outcome": "error"})},
	})

	stdout, _, err := captureStdoutStderr(t, (&NextCmd{Task: "fix/interrupted"}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Last run interrupted")
	require.Contains(t, stdout, `subtask send fix/interrupted "..."`)
	require.NotContains(t, stdout, "subtask trace")

	stdout, _, err = captureStdoutStderr(t, (&NextCmd{Task: "fix/error"}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Last run failed: boom")
	require.Contains(t, stdout, "subtask log fix/error")
	require.Contains(t, stdout, "subtask trace fix/error")
}

func TestNext_TerminalTaskStatuses(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	env.CreateTask("fix/merged", "Merged task", "main", "Done")
	env.CreateTaskHistory("fix/merged", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{Type: "task.merged", Data: mustJSON(map[string]any{"commit": "1234567890abcdef", "into": "main"})},
	})
	env.CreateTask("fix/closed", "Closed task", "main", "Done")
	env.CreateTaskHistory("fix/closed", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
		{Type: "task.closed", Data: mustJSON(map[string]any{"reason": "close"})},
	})

	stdout, _, err := captureStdoutStderr(t, (&NextCmd{Task: "fix/merged"}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Task merged")
	require.Contains(t, stdout, "No next worker action")

	stdout, _, err = captureStdoutStderr(t, (&NextCmd{Task: "fix/closed"}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Task closed")
	require.Contains(t, stdout, "No next worker action")
}

func TestNext_RoutineGuidanceAndTerminalStep(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "guided.yaml"), []byte(
		`name: guided
steps:
  - id: plan
    instructions: |
      Read <task>.
  - id: ready
    kind: terminal
`), 0o644))

	require.NoError(t, (&DraftCmd{
		Task:        "fix/routine",
		Title:       "Routine task",
		Description: "Do it",
		Base:        "main",
		Routine:     "guided",
	}).Run())
	stdout, _, err := captureStdoutStderr(t, (&NextCmd{Task: "fix/routine"}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Step:")
	require.Contains(t, stdout, "Plan:")
	require.Contains(t, stdout, "  Read fix/routine.")

	require.NoError(t, (&StageCmd{Task: "fix/routine", Stage: "ready", NoSend: true, Quiet: true}).Run())
	stdout, _, err = captureStdoutStderr(t, (&NextCmd{Task: "fix/routine"}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, `Routine is at terminal step "ready"`)
	require.Contains(t, stdout, "subtask merge fix/routine")
	require.NotContains(t, stdout, "Read fix/routine")
}

func TestNext_NonRoutineSkipsRoutineGuidance(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	env.CreateTask("fix/plain", "Plain task", "main", "Do it")
	env.CreateTaskHistory("fix/plain", []history.Event{
		{Type: "task.opened", Data: mustJSON(map[string]any{"reason": "draft", "base_branch": "main"})},
	})

	stdout, _, err := captureStdoutStderr(t, (&NextCmd{Task: "fix/plain"}).Run)
	require.NoError(t, err)
	require.Contains(t, stdout, "Task drafted")
	require.NotContains(t, stdout, "Step:")
}
