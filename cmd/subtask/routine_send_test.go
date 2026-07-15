package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/store"
	"github.com/kgruel/subtask/pkg/testutil"
)

// TestSend_RoutineLoopbackHitsDispatchCap is the regression for the P1
// bug codex caught: routine branches support loopbacks (a step can have
// a `branches:` edge back to itself), so the auto-advance recursion in
// send.go is NOT bounded by step count. Without a cap, a routine whose
// produced artifact keeps satisfying the loopback predicate would loop
// forever, hammering the worker.
//
// The fix bounds the recursion at autoDispatchCap (25). This test sets
// up exactly that pathology and asserts the cap fires after 25 rounds.
func TestSend_RoutineLoopbackHitsDispatchCap(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	// Routine: single agent-driven step that loops back to itself when
	// the produced artifact's `needs_loop` flag is true.
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "looper.yaml"), []byte(
		`name: looper
steps:
  - id: plan
    agent: planner
    produces: PLAN.md
    advance: auto
    branches:
      - to: plan
        when: artifact.field
        field: needs_loop
  - id: done
    kind: terminal
`), 0o644))

	// Agent: minimal — adapter must match cfg so no cross-adapter swap
	// fires on every loop iteration (that would also clear the session,
	// which is fine but adds noise).
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(
		`adapter: builtin-mock
model: m
prompt:
  text: |
    You are the planner.
`), 0o644))

	taskName := "lp/cap"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Loopback cap",
		Description: "Regression: routine loopback must hit autoDispatchCap",
		Base:        "main",
		Routine:     "looper",
	}).Run())

	// Pre-write the produced artifact with needs_loop: true. The mock
	// harness doesn't write files; the runner reads this on every
	// HandleAutoAdvance call and keeps choosing the loopback branch.
	taskDir := task.Dir(taskName)
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "PLAN.md"),
		[]byte("---\nneeds_loop: true\n---\n"), 0o644))

	mock := harness.NewMockHarness().WithResult("ok", "sess-loop")
	err := (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run()

	// The cap MUST fire — otherwise the test would never return.
	require.Error(t, err, "loopback routine must hit the dispatch cap")
	require.Contains(t, err.Error(), "auto-advance dispatch limit reached")
	require.Contains(t, err.Error(), "25 rounds")

	// Worker must have run exactly autoDispatchCap (25) times: depths
	// 0..24 each entered SendCmd.Run, each performed a worker.Run, and
	// the 25th's HandleAutoAdvance detected that the next recursion
	// would be the 26th round and bailed.
	require.Equal(t, autoDispatchCap, mock.RunCallCount(),
		"worker should have run exactly %d times before the cap fired", autoDispatchCap)
}

// TestDraft_RoutineRejectsBrokenLaterStep is the regression for the
// fail-fast-at-draft requirement: a typo in step 3's agent: must error
// at draft, before any task state is written, not at the third worker
// round mid-routine.
func TestDraft_RoutineRejectsBrokenLaterStep(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	// Working agent for the entry step.
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "good.yaml"), []byte(
		"adapter: claude\nmodel: m\nprompt:\n  text: |\n    You are good.\n"), 0o644))

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "with-typo.yaml"), []byte(
		`name: with-typo
steps:
  - id: a
    agent: good
    advance: auto
  - id: b
    agent: ghost-typo
    advance: auto
  - id: done
    kind: terminal
`), 0o644))

	err := (&DraftCmd{
		Task:        "draft/broken",
		Title:       "Routine with typo in later step",
		Description: "Draft must fail before any task state is written",
		Base:        "main",
		Routine:     "with-typo",
	}).Run()
	require.Error(t, err, "draft must reject routine with broken later-step agent")
	require.Contains(t, err.Error(), "ghost-typo")
	require.Contains(t, err.Error(), `step "b"`)

	// No half-written state: the task folder must not exist.
	_, statErr := os.Stat(task.Dir("draft/broken"))
	require.True(t, os.IsNotExist(statErr), "no task folder must be created when draft fails validation")
}

// TestShow_RoutineRendersProgression verifies the P2 fix for `subtask
// show` on routine tasks: store.Get loads the routine, and show
// renders the routine name + step progression.
func TestShow_RoutineRendersProgression(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(
		"adapter: claude\nmodel: m\nprompt:\n  text: |\n    Plan.\n"), 0o644))

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "show.yaml"), []byte(
		`name: show
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    agent: planner
    advance: auto
  - id: done
    kind: terminal
`), 0o644))

	taskName := "show/routine"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Routine show",
		Description: "Show should render routine progression",
		Base:        "main",
		Routine:     "show",
	}).Run())

	// Detail-level check: routine is loaded.
	detail, err := store.New().Get(context.Background(), taskName, store.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, detail.Routine, "store.Get must load the routine when t.Routine is set")
	require.Equal(t, "show", detail.Routine.Name)
	require.Equal(t, "plan", detail.Stage, "draft should initialize stage to entry step")

	// show output: routine name + step progression rendered.
	out, _, err := captureStdoutStderr(t, (&ShowCmd{Task: taskName}).Run)
	require.NoError(t, err)
	require.Contains(t, out, "show", "show output must name the routine")
	// Step progression: current step highlighted via FormatStageProgression.
	require.Contains(t, out, "plan")
	require.Contains(t, out, "review")
	require.Contains(t, out, "done")
	// No phantom "Workflow:" label for routine tasks.
	require.False(t, strings.Contains(out, "Workflow:"),
		"routine task must not surface a Workflow: label")
}

// TestSend_RoutineAutoDispatchInheritsQuiet is the regression for the
// codex round-3 P2: the recursive SendCmd at the routine auto-advance
// site must inherit the user's --quiet flag. Without it, an
// auto-advanced round switches back to full formatted worker output
// mid-chain, surprising the user who explicitly asked for quiet.
//
// Test the inheritance via the user-visible side-effect: in quiet
// mode, only `reply\n` is printed per round; in non-quiet mode, the
// "─── Reply ───" / "─── History ───" formatted sections appear. We
// run a routine that auto-advances once and assert no formatted
// section markers leak into the output.
func TestSend_RoutineAutoDispatchInheritsQuiet(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "a.yaml"), []byte(
		"adapter: builtin-mock\nmodel: m\nprompt:\n  text: |\n    role A\n"), 0o644))

	// Two regular agent steps so the first reply triggers exactly one
	// auto-dispatch — enough to exercise the recursion site without
	// approaching the dispatch cap.
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "two-step.yaml"), []byte(
		`name: two-step
steps:
  - id: a
    agent: a
    advance: auto
  - id: b
    agent: a
    advance: auto
  - id: done
    kind: terminal
`), 0o644))

	taskName := "rt/quiet-prop"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Routine quiet propagation",
		Description: "Auto-advance must inherit --quiet",
		Base:        "main",
		Routine:     "two-step",
	}).Run())

	mock := harness.NewMockHarness().WithResult("ok", "sess-q")
	out, _, err := captureStdoutStderr(t, (&SendCmd{Task: taskName, Prompt: "Go", Quiet: true}).WithHarness(mock).Run)
	require.NoError(t, err)

	// Two worker rounds happened (initial + one auto-dispatch).
	require.Equal(t, 2, mock.RunCallCount(), "two rounds should have run (initial + one auto-dispatch)")

	// Quiet mode prints only the reply body, no section banners. If the
	// recursion dropped Quiet, the second round would surface
	// "─── Reply ───" or "─── Workspace ───" markers.
	require.NotContains(t, out, "─── Reply ───", "quiet mode must not emit Reply banner on any round")
	require.NotContains(t, out, "─── Workspace ───", "quiet mode must not emit Workspace banner on any round")
	require.NotContains(t, out, "─── History ───", "quiet mode must not emit History banner on any round")
	// Defensive: the "Sending to task" / "[Waiting for worker...]" info
	// lines are also suppressed in quiet mode.
	require.NotContains(t, out, "Sending to task", "quiet mode must not emit info lines")
	require.NotContains(t, out, "Waiting for worker", "quiet mode must not emit info lines")
}

// TestDraft_RoutineAndAgentMutex is the regression for the codex
// round-4 P2: --routine and --agent are mutually exclusive. A routine
// task's per-step agent is the source of truth for the worker prompt's
// ## Agent block; combining the two flags would persist a t.Agent that
// harness.BuildPrompt deliberately ignores for routine tasks, producing
// mixed state (worker runs with the agent's adapter/model but reads the
// routine step's role prompt).
//
// Also asserts two positive cases: routine alone and agent alone.
func TestDraft_RoutineAndAgentMutex(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "a.yaml"), []byte(
		"adapter: claude\nmodel: m\nprompt:\n  text: |\n    Hello.\n"), 0o644))

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "r.yaml"), []byte(
		`name: r
steps:
  - id: plan
    agent: a
    advance: auto
  - id: done
    kind: terminal
`), 0o644))

	t.Run("routine + agent rejected", func(t *testing.T) {
		err := (&DraftCmd{
			Task:        "mutex/both",
			Title:       "T",
			Description: "Should error before any state is written",
			Base:        "main",
			Routine:     "r",
			Agent:       "a",
		}).Run()
		require.Error(t, err)
		require.Contains(t, err.Error(), "--agent and --routine are mutually exclusive")
		require.Contains(t, err.Error(), "step config")

		_, statErr := os.Stat(task.Dir("mutex/both"))
		require.True(t, os.IsNotExist(statErr), "rejected combination must leave no task folder behind")
	})

	t.Run("routine alone works", func(t *testing.T) {
		require.NoError(t, (&DraftCmd{
			Task:        "mutex/routine-only",
			Title:       "T",
			Description: "Routine alone is fine",
			Base:        "main",
			Routine:     "r",
		}).Run())
		tk, err := task.Load("mutex/routine-only")
		require.NoError(t, err)
		require.Equal(t, "r", tk.Routine)
		require.Empty(t, tk.Agent, "routine task must not persist t.Agent")
	})

	t.Run("agent alone works", func(t *testing.T) {
		require.NoError(t, (&DraftCmd{
			Task:        "mutex/agent-only",
			Title:       "T",
			Description: "Agent alone (step 3 path)",
			Base:        "main",
			Agent:       "a",
		}).Run())
		tk, err := task.Load("mutex/agent-only")
		require.NoError(t, err)
		require.Equal(t, "a", tk.Agent)
		require.Empty(t, tk.Routine)
	})

}

// TestSend_AutoAdvanceDecisionError_StampsLastError is the item-A regression:
// when post-reply auto-advance fails (here HandleAutoAdvance errors on a
// malformed produces artifact), the failure must be recorded durably in
// state.LastError — not merely returned. Otherwise the just-committed
// worker.finished(outcome=replied) plus the cleared LastError leave the failure
// invisible to wait/show/list/unread: under --detach it would only reach the
// supervisor log, and under -q it would be swallowed entirely.
func TestSend_AutoAdvanceDecisionError_StampsLastError(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	// Routine whose advance:auto step reads a produces artifact to pick its
	// loopback branch. A malformed artifact makes pickNextStep (thus
	// HandleAutoAdvance) error after the worker's reply is committed.
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "advbad.yaml"), []byte(
		`name: advbad
steps:
  - id: work
    agent: planner
    produces: PLAN.md
    advance: auto
    branches:
      - to: work
        when: artifact.field
        field: needs_rework
  - id: done
    kind: terminal
`), 0o644))

	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(
		"adapter: builtin-mock\nmodel: m\nprompt:\n  text: |\n    You are the planner.\n"), 0o644))

	taskName := "adv/baddecision"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Auto-advance decision error",
		Description: "malformed produces artifact makes HandleAutoAdvance error",
		Base:        "main",
		Routine:     "advbad",
	}).Run())

	// Unterminated frontmatter → readArtifactBool (thus HandleAutoAdvance) errors.
	require.NoError(t, os.WriteFile(filepath.Join(task.Dir(taskName), "PLAN.md"),
		[]byte("---\nneeds_rework: true"), 0o644))

	mock := harness.NewMockHarness().WithResult("ok", "sess-adv")
	err := (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(mock).Run()
	require.Error(t, err, "a failed auto-advance decision must surface as an error")
	require.Contains(t, err.Error(), "unclosed YAML frontmatter")

	// Item A: the failure is durably recorded so detached/quiet callers see it.
	st, loadErr := task.LoadState(taskName)
	require.NoError(t, loadErr)
	require.NotNil(t, st)
	require.Contains(t, st.LastError, "auto-advance failed:")
	require.Equal(t, 0, st.SupervisorPID, "the supervisor claim must be cleared")

	// wait must exit 2 via the LastError row — independent of the auto-advance
	// classifier, which remains belt-and-braces.
	code, waitErr, stdout, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{taskName}, Any: true})
	require.NoError(t, waitErr)
	require.Equal(t, 2, code, "a durably-recorded auto-advance failure is complete-with-error")
	require.Contains(t, stdout, taskName+"\terror")
}

// corruptingHarness replies normally but corrupts a file mid-run, simulating
// the routine being edited/removed while the worker works. The corruption must
// land inside the run window: prompt-building loads the routine too, so
// corrupting it before send would fail the send early and never reach the
// post-reply reload this test is about.
type corruptingHarness struct {
	*harness.MockHarness
	path    string
	content string
}

func (h *corruptingHarness) Run(ctx context.Context, cwd, prompt, continueFrom string, cb harness.Callbacks) (*harness.Result, error) {
	if err := os.WriteFile(h.path, []byte(h.content), 0o644); err != nil {
		return nil, err
	}
	return h.MockHarness.Run(ctx, cwd, prompt, continueFrom, cb)
}

// Sibling of the above for the other post-reply failure mode: the routine
// itself becomes unloadable (removed or corrupted) while the worker runs, so
// the advance decision can't even be attempted. Same durability contract — a
// bare return would leave a clean "replied" with no LastError, and wait would
// report a clean reply that never happened.
func TestSend_AutoAdvanceRoutineReloadError_StampsLastError(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	routinePath := filepath.Join(routinesDir, "advgone.yaml")
	require.NoError(t, os.WriteFile(routinePath, []byte(
		`name: advgone
steps:
  - id: work
    advance: auto
  - id: done
    kind: terminal
`), 0o644))

	taskName := "adv/routinegone"
	require.NoError(t, (&DraftCmd{
		Task:        taskName,
		Title:       "Routine unloadable at advance time",
		Description: "routine corrupted while the worker ran",
		Base:        "main",
		Routine:     "advgone",
	}).Run())

	h := &corruptingHarness{
		MockHarness: harness.NewMockHarness().WithResult("ok", "sess-gone"),
		path:        routinePath,
		content:     "name: advgone\nsteps: [[[\n",
	}
	err := (&SendCmd{Task: taskName, Prompt: "Go"}).WithHarness(h).Run()
	require.Error(t, err, "an unloadable routine must surface as an error")
	require.Contains(t, err.Error(), "invalid YAML")

	st, loadErr := task.LoadState(taskName)
	require.NoError(t, loadErr)
	require.NotNil(t, st)
	require.Contains(t, st.LastError, "auto-advance failed:")
	require.Equal(t, 0, st.SupervisorPID, "the supervisor claim must be released exactly once")

	code, waitErr, stdout, _ := runWaitCapture(t, &WaitCmd{Tasks: []string{taskName}, Any: true})
	require.NoError(t, waitErr)
	require.Equal(t, 2, code, "the recorded failure must not read as a clean reply")
	require.Contains(t, stdout, taskName+"\terror")
}
