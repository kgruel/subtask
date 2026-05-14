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
	"github.com/kgruel/subtask/pkg/task/gather"
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
		`preset:
  adapter: builtin-mock
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
		"preset:\n  adapter: claude\n  model: m\nprompt:\n  text: |\n    You are good.\n"), 0o644))

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
// show` on routine tasks: gather.Detail loads the routine, and show
// renders the routine name + step progression.
func TestShow_RoutineRendersProgression(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(
		"preset:\n  adapter: claude\n  model: m\nprompt:\n  text: |\n    Plan.\n"), 0o644))

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
	detail, err := gather.Detail(context.Background(), taskName)
	require.NoError(t, err)
	require.NotNil(t, detail.Routine, "gather.Detail must load the routine when t.Routine is set")
	require.Equal(t, "show", detail.Routine.Name)
	require.Equal(t, "plan", detail.Stage, "draft should initialize stage to entry step")
	require.Nil(t, detail.Workflow, "routine tasks have no workflow")

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
		"preset:\n  adapter: builtin-mock\n  model: m\nprompt:\n  text: |\n    role A\n"), 0o644))

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
// Also asserts the three positive cases the brief calls out: routine
// alone, agent alone (step 3's path), and workflow+agent (step 3's
// ad-hoc agent dispatch). The last is the trickiest — it must continue
// to work.
func TestDraft_RoutineAndAgentMutex(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	withOutputMode(t, false)

	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "a.yaml"), []byte(
		"preset:\n  adapter: claude\n  model: m\nprompt:\n  text: |\n    Hello.\n"), 0o644))

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
		// Actionable hint must mention the per-step config and the
		// workflow+agent escape hatch.
		require.Contains(t, err.Error(), "step config")
		require.Contains(t, err.Error(), "--workflow")

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

	t.Run("workflow + agent allowed", func(t *testing.T) {
		// Step 3 deliberately allows --workflow + --agent for ad-hoc
		// agent dispatch on top of the default workflow. This must
		// continue to work.
		require.NoError(t, (&DraftCmd{
			Task:        "mutex/workflow-agent",
			Title:       "T",
			Description: "Workflow + agent ad-hoc dispatch",
			Base:        "main",
			Workflow:    "default",
			Agent:       "a",
		}).Run())
		tk, err := task.Load("mutex/workflow-agent")
		require.NoError(t, err)
		require.Equal(t, "a", tk.Agent)
		require.Empty(t, tk.Routine)
	})
}
