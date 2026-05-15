package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

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

// Routine flavor of TestUnread_SilentStage_NotUnread: a routine step
// with `notify: false` must also suppress unread, evaluated against
// the stage stamped on worker.finished (not the current tail.Stage).
func TestUnread_RoutineSilentStep_NotUnread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "silent.yaml"), []byte(
		`name: silent
steps:
  - id: implement
  - id: commit
    notify: false
  - id: done
    kind: terminal
`), 0o644))

	taskName := "fix/routine-silent"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Routine silent step",
		Description: "notify:false on a routine step",
		Base:        "main",
		Routine:     "silent",
	}).Run())

	// Move to the silent commit step BEFORE sending so the reply is
	// stamped with stage=commit (notify:false).
	require.NoError(t, (&StageCmd{Task: taskName, Stage: "commit", NoSend: true}).Run())

	mock := harness.NewMockHarness().WithResult("ok", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread, "reply stamped with routine step notify:false must be silenced")
}

// Brief P2 scenario: routine where a silent step auto-advances into a
// non-silent successor. The reply must remain silenced even though
// tail.Stage moves to a non-silent step — the event-sourced policy
// reads the reply's stamped stage. Before the round-6 fix the reply
// would surface as unread once auto-advance landed on the
// non-silent step.
func TestUnread_RoutineSilentReplyAutoAdvancesToNonSilent_NotUnread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	// silent (notify:false, auto) → quiet-next (passive, no dispatch)
	// → done (terminal). The passive next step means auto-advance fires
	// once and stops (no recursion) — keeps the test deterministic.
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "auto.yaml"), []byte(
		`name: auto
steps:
  - id: silent
    notify: false
    advance: auto
  - id: quiet-next
  - id: done
    kind: terminal
`), 0o644))

	taskName := "fix/routine-silent-auto"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Silent step auto-advances",
		Description: "Silent reply must stay silent after auto-advance",
		Base:        "main",
		Routine:     "auto",
	}).Run())

	mock := harness.NewMockHarness().WithResult("ok", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	// Confirm the task auto-advanced into the non-silent successor.
	tail, err := history.Tail(taskName)
	require.NoError(t, err)
	require.Equal(t, "quiet-next", tail.Stage,
		"routine should auto-advance from silent into quiet-next")

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread,
		"silent reply must NOT surface as unread after auto-advance into a non-silent step")
}

// Mirror of the above: routine where a non-silent step auto-advances
// into a silent successor. The reply was made in a non-silent step
// and must surface — the destination's silence does not retroactively
// silence prior replies.
func TestUnread_RoutineNonSilentReplyAutoAdvancesToSilent_Unread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "noisy.yaml"), []byte(
		`name: noisy
steps:
  - id: noisy
    advance: auto
  - id: silent-next
    notify: false
  - id: done
    kind: terminal
`), 0o644))

	taskName := "fix/routine-noisy-auto"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Non-silent reply, then silent successor",
		Description: "Mirror of the silent-auto-advance scenario",
		Base:        "main",
		Routine:     "noisy",
	}).Run())

	mock := harness.NewMockHarness().WithResult("ok", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	tail, err := history.Tail(taskName)
	require.NoError(t, err)
	require.Equal(t, "silent-next", tail.Stage)

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.True(t, unread,
		"non-silent reply must surface even when the successor step is notify:false")
}

// Routine terminal step with `surface: false` must suppress unread
// for replies stamped with that terminal as their stage.
func TestUnread_RoutineTerminalSurfaceFalse_NotUnread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "term.yaml"), []byte(
		`name: term
steps:
  - id: implement
  - id: cancelled
    kind: terminal
    surface: false
`), 0o644))

	taskName := "fix/routine-term-silent"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Routine surface:false terminal",
		Description: "surface:false terminal",
		Base:        "main",
		Routine:     "term",
	}).Run())

	// Advance to the surface:false terminal, then send. The reply's
	// stamped stage is `cancelled` (surface:false) and must be silenced.
	require.NoError(t, (&StageCmd{Task: taskName, Stage: "cancelled", NoSend: true}).Run())

	mock := harness.NewMockHarness().WithResult("ok", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread,
		"reply stamped with routine terminal surface:false must not be unread")
}

// Routine handoff scenarios (round-8 P1): auto-advance into a
// surfaced gate / terminal must surface to `subtask unread` even
// though no new worker.finished fires after the transition. Without
// the routine.surfaced event, gates and terminals were invisible to
// the lead — the round-7 reply-stage policy silenced the prior
// worker.finished (correctly, when the reply was in a silent step)
// but nothing else marked the handoff.

func TestUnread_RoutineAutoAdvanceIntoSurfacedGate_Unread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	// silent (notify:false, advance:auto) → review (gate, default
	// surface:true). After the worker reply, the runner auto-advances
	// into the gate and emits routine.surfaced. Even though the reply
	// itself is silenced (silent step), the gate handoff must surface.
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "handoff.yaml"), []byte(
		`name: handoff
steps:
  - id: silent
    notify: false
    advance: auto
  - id: review
    kind: gate
    options:
      - { name: approve, next: done }
      - { name: cancel,  next: cancelled }
  - id: done
    kind: terminal
  - id: cancelled
    kind: terminal
    surface: false
`), 0o644))

	taskName := "fix/gate-handoff"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Silent → surfaced gate",
		Description: "Gate handoff must reach subtask unread",
		Base:        "main",
		Routine:     "handoff",
	}).Run())

	mock := harness.NewMockHarness().WithResult("ok", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	tail, err := history.Tail(taskName)
	require.NoError(t, err)
	require.Equal(t, "review", tail.Stage, "routine should auto-advance into the gate")

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.True(t, unread, "auto-advance into a surfaced gate must mark the task as unread")
}

func TestUnread_RoutineAutoAdvanceIntoSurfaceFalseGate_NotUnread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "handoff-quiet.yaml"), []byte(
		`name: handoff-quiet
steps:
  - id: silent
    notify: false
    advance: auto
  - id: review
    kind: gate
    surface: false
    options:
      - { name: approve, next: done }
  - id: done
    kind: terminal
`), 0o644))

	taskName := "fix/gate-handoff-silent"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Silent → surface:false gate",
		Description: "surface:false suppresses handoff",
		Base:        "main",
		Routine:     "handoff-quiet",
	}).Run())

	mock := harness.NewMockHarness().WithResult("ok", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread,
		"surface:false gate must NOT mark the task as unread on auto-advance entry")
}

func TestUnread_RoutineAutoAdvanceIntoSurfacedTerminal_Unread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "term-done.yaml"), []byte(
		`name: term-done
steps:
  - id: silent
    notify: false
    advance: auto
  - id: done
    kind: terminal
`), 0o644))

	taskName := "fix/term-done-handoff"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Silent → surfaced terminal",
		Description: "Routine completion must reach subtask unread",
		Base:        "main",
		Routine:     "term-done",
	}).Run())

	mock := harness.NewMockHarness().WithResult("ok", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.True(t, unread,
		"auto-advance into a surfaced terminal (default surface:true) must mark the task as unread")
}

func TestUnread_RoutineAutoAdvanceIntoSurfaceFalseTerminal_NotUnread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "term-cancel.yaml"), []byte(
		`name: term-cancel
steps:
  - id: silent
    notify: false
    advance: auto
  - id: cancelled
    kind: terminal
    surface: false
`), 0o644))

	taskName := "fix/term-cancel-handoff"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Silent → surface:false terminal",
		Description: "Suppressed terminal must stay quiet",
		Base:        "main",
		Routine:     "term-cancel",
	}).Run())

	mock := harness.NewMockHarness().WithResult("ok", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread,
		"surface:false terminal must NOT mark the task as unread on auto-advance entry")
}

// A lead message AFTER the surface event clears the unread state —
// the lead has engaged with the task.
func TestUnread_LeadMessageAfterSurfaceEvent_ClearsUnread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "lead-ack.yaml"), []byte(
		`name: lead-ack
steps:
  - id: silent
    notify: false
    advance: auto
  - id: done
    kind: terminal
`), 0o644))

	taskName := "fix/lead-ack"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Lead ack clears surface event",
		Description: "Lead message after surface clears unread",
		Base:        "main",
		Routine:     "lead-ack",
	}).Run())

	mock := harness.NewMockHarness().WithResult("ok", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	// Surface event surfaces: unread is true.
	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.True(t, unread, "surfaced terminal should be unread before lead engagement")

	// Lead message clears it.
	require.NoError(t, history.Append(taskName, history.Event{
		Type:    "message",
		Role:    "lead",
		Content: "got it",
	}))
	unread, err = taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread, "lead message after surface event must clear unread")
}

// Workflow tasks must NOT see routine.surfaced events (none are
// emitted on the workflow path). Behavior unchanged.
// Routine gate step with `surface: false` must also suppress unread.
// The schema (Step.Surface comment) documents surface: false applying
// to both terminal AND gate steps; earlier this check honored it only
// on terminals, so a reply stamped during a surface:false gate
// incorrectly surfaced as unread.
func TestUnread_RoutineGateSurfaceFalse_NotUnread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "gate-quiet.yaml"), []byte(
		`name: gate-quiet
steps:
  - id: implement
  - id: review
    kind: gate
    surface: false
    options:
      - { name: approve, next: done }
      - { name: revise,  next: implement }
  - id: done
    kind: terminal
`), 0o644))

	taskName := "fix/routine-gate-silent"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Routine gate surface:false",
		Description: "Gate surface:false should silence",
		Base:        "main",
		Routine:     "gate-quiet",
	}).Run())

	// Advance to the surface:false gate, then send. The reply's stamped
	// stage is `review` (gate, surface:false) and must be silenced.
	require.NoError(t, (&StageCmd{Task: taskName, Stage: "review", NoSend: true}).Run())

	mock := harness.NewMockHarness().WithResult("ok", "sess-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run())

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread,
		"reply stamped with routine gate surface:false must not be unread")
}

// Legacy fallback: worker.finished events without the `stage` field
// (pre-step-4 history, or any other producer that hasn't been updated)
// must fall back to evaluating the policy against the current tail
// stage. The test writes such an event manually and asserts the
// fallback path silences correctly.
func TestUnread_LegacyWorkerFinishedFallsBackToTailStage(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "legacy.yaml"), []byte(
		`name: legacy
steps:
  - id: muted
    notify: false
  - id: done
    kind: terminal
`), 0o644))

	taskName := "fix/legacy-no-stage"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Legacy worker.finished",
		Description: "stage field missing on worker.finished",
		Base:        "main",
		Routine:     "legacy",
	}).Run())

	// Append a worker message + a worker.finished event WITHOUT the
	// stage field (simulating pre-step-4 history). The current tail
	// stage is `muted` (notify:false), so the fallback path must
	// suppress unread.
	now := time.Now().UTC()
	require.NoError(t, history.Append(taskName, history.Event{
		Type: "message", Role: "worker", Content: "ok", TS: now,
	}))
	legacyData, _ := json.Marshal(map[string]any{
		"run_id":      "legacy-run",
		"duration_ms": 10,
		"tool_calls":  0,
		"outcome":     "replied",
		// NOTE: stage field intentionally omitted.
	})
	require.NoError(t, history.Append(taskName, history.Event{
		Type: "worker.finished", Data: legacyData, TS: now,
	}))

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread,
		"legacy worker.finished without stage must fall back to current tail.Stage policy")
}

func TestUnread_FreshTask_NotUnread(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "fix/fresh"
	env.CreateTask(taskName, "Drafted, never sent", "main", "")

	unread, err := taskHasUnreadReply(taskName)
	require.NoError(t, err)
	assert.False(t, unread, "drafted-only task with no worker.finished should not be unread")
}
