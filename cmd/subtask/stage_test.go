package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

// TestStage_QuietSuppressesInstructions verifies that -q suppresses the routine
// diagram and step instructions on the passive (no auto-dispatch) path while
// still printing the transition line.
func TestStage_QuietSuppressesInstructions(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "guided.yaml"), []byte(
		`name: guided
steps:
  - id: work
    instructions: |
      Review the diff carefully and approve.
  - id: done
    kind: terminal
`), 0o644))

	taskName := "fix/quiet-stage"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Quiet stage test",
		Description: "Verify -q suppresses instructions",
		Base:        "main",
		Routine:     "guided",
	}).Run())

	stdout, _, err := captureStdoutStderr(t, func() error {
		return (&StageCmd{Task: taskName, Stage: "work", NoSend: true, Quiet: true}).Run()
	})
	require.NoError(t, err)
	require.Contains(t, stdout, "work", "transition line must mention step id")
	require.NotContains(t, stdout, "Review the diff carefully", "instructions must be suppressed by -q")
}

// TestStage_SameStepNoOp verifies that staging a task to the step it is
// already on prints "already on step <id>" and does NOT append a
// stage.changed history event.
func TestStage_SameStepNoOp(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "two-step.yaml"), []byte(
		`name: two-step
steps:
  - id: work
  - id: done
    kind: terminal
`), 0o644))

	taskName := "fix/same-step"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Same-step test",
		Description: "Verify stage no-op when already on step",
		Base:        "main",
		Routine:     "two-step",
	}).Run())

	// Advance to "work".
	require.NoError(t, (&StageCmd{Task: taskName, Stage: "work", NoSend: true}).Run())

	countStageChanged := func(taskName string) int {
		events, err := history.Read(taskName, history.ReadOptions{})
		require.NoError(t, err)
		n := 0
		for _, ev := range events {
			if ev.Type == "stage.changed" {
				n++
			}
		}
		return n
	}

	// Confirm the stage.changed event was written once.
	require.Equal(t, 1, countStageChanged(taskName), "one stage.changed event after first transition")

	// Stage to the same step again — should be a no-op.
	stdout, _, err := captureStdoutStderr(t, func() error {
		return (&StageCmd{Task: taskName, Stage: "work", NoSend: true}).Run()
	})
	require.NoError(t, err)
	require.Contains(t, stdout, "already on step work")

	// History must NOT have grown.
	require.Equal(t, 1, countStageChanged(taskName), "no new stage.changed event on same-step no-op")
}
