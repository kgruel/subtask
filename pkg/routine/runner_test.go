package routine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

// helperRoutine parses YAML and fails the test on error.
func helperRoutine(t *testing.T, data string) *Routine {
	t.Helper()
	r, err := parseRoutine([]byte(data))
	require.NoError(t, err)
	return r
}

// helperWriteArtifact writes a file with YAML frontmatter into the task
// folder so HandleAutoAdvance's branch evaluation can read it.
func helperWriteArtifact(t *testing.T, taskName, relPath string, frontmatter map[string]any, body string) {
	t.Helper()
	dir := task.Dir(taskName)
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, relPath)), 0o755))
	b, err := yaml.Marshal(frontmatter)
	require.NoError(t, err)
	content := "---\n" + string(b) + "---\n" + body
	require.NoError(t, os.WriteFile(filepath.Join(dir, relPath), []byte(content), 0o644))
}

// helperCreateTask creates a task folder + minimal TASK.md so
// history.AppendLocked and task.Load have somewhere to write.
func helperCreateTask(t *testing.T, env *testutil.TestEnv, name string) *task.Task {
	t.Helper()
	return env.CreateTask(name, "title", "main", "desc")
}

// linearTwoStep is the canonical fixture: agent step → terminal.
const linearTwoStep = `name: linear2
steps:
  - id: plan
    agent: planner
    produces: PLAN.md
    advance: auto
  - id: done
    kind: terminal
`

const branchLoopback = `name: loop
steps:
  - id: plan
    agent: planner
    produces: PLAN.md
    advance: auto
    branches:
      - to: plan
        when: artifact.field
        field: needs_rework
  - id: done
    kind: terminal
`

const gateStop = `name: gated
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    kind: gate
    options:
      - { name: approve, next: done }
      - { name: revise,  next: plan }
  - id: done
    kind: terminal
`

// plainStep has no agent and no worker_instructions — should not auto-dispatch.
const plainStep = `name: plain
steps:
  - id: explore
    agent: explorer
    advance: auto
  - id: impl
    advance: auto
  - id: done
    kind: terminal
`

func mkAgent(t *testing.T, env *testutil.TestEnv, name, adapter, model string) {
	t.Helper()
	dir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(
		"adapter: "+adapter+"\nmodel: "+model+"\nprompt:\n  text: You are "+name+".\n"), 0o644))
}

// TestHandleAutoAdvance_LinearAdvance: no branches, default-advance to next step.
func TestHandleAutoAdvance_LinearAdvance(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "claude", "opus")
	helperCreateTask(t, env, "lin/adv")

	r := helperRoutine(t, linearTwoStep)

	res, err := HandleAutoAdvance("lin/adv", r, "plan", time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "done", res.NextStep, "should default-advance to next in declaration order")
	require.False(t, res.Dispatch, "terminal steps never dispatch")
}

// TestHandleAutoAdvance_BranchTakenWhenFieldTrue: branches with bool=true → branch.to taken.
func TestHandleAutoAdvance_BranchTakenWhenFieldTrue(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "claude", "opus")
	helperCreateTask(t, env, "br/true")
	helperWriteArtifact(t, "br/true", "PLAN.md",
		map[string]any{"needs_rework": true},
		"")

	r := helperRoutine(t, branchLoopback)

	res, err := HandleAutoAdvance("br/true", r, "plan", time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "plan", res.NextStep, "branch field=true should loop back to plan")
	require.True(t, res.Dispatch, "agent-driven regular step must auto-dispatch")
}

// TestHandleAutoAdvance_BranchSkippedWhenFieldFalse: field=false → default-advance.
func TestHandleAutoAdvance_BranchSkippedWhenFieldFalse(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "claude", "opus")
	helperCreateTask(t, env, "br/false")
	helperWriteArtifact(t, "br/false", "PLAN.md",
		map[string]any{"needs_rework": false},
		"")

	r := helperRoutine(t, branchLoopback)

	res, err := HandleAutoAdvance("br/false", r, "plan", time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "done", res.NextStep, "field=false should fall through to declaration order")
}

// TestHandleAutoAdvance_BranchSkippedWhenFieldAbsent: field missing → default-advance.
func TestHandleAutoAdvance_BranchSkippedWhenFieldAbsent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "claude", "opus")
	helperCreateTask(t, env, "br/absent")
	helperWriteArtifact(t, "br/absent", "PLAN.md",
		map[string]any{"other_field": true},
		"")

	r := helperRoutine(t, branchLoopback)

	res, err := HandleAutoAdvance("br/absent", r, "plan", time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "done", res.NextStep)
}

// TestHandleAutoAdvance_BranchSkippedWhenArtifactMissing: file missing → default-advance.
func TestHandleAutoAdvance_BranchSkippedWhenArtifactMissing(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "claude", "opus")
	helperCreateTask(t, env, "br/missing")
	// No PLAN.md written.

	r := helperRoutine(t, branchLoopback)

	res, err := HandleAutoAdvance("br/missing", r, "plan", time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "done", res.NextStep)
}

// TestHandleAutoAdvance_TerminalStops: current step terminal → no advance.
func TestHandleAutoAdvance_TerminalStops(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "claude", "opus")
	helperCreateTask(t, env, "term/stops")

	r := helperRoutine(t, linearTwoStep)

	res, err := HandleAutoAdvance("term/stops", r, "done", time.Now().UTC())
	require.NoError(t, err)
	require.Empty(t, res.NextStep, "terminal step must not advance")
}

// TestHandleAutoAdvance_GateNeverDispatches: advancing into a gate stops dispatch.
func TestHandleAutoAdvance_GateNeverDispatches(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "claude", "opus")
	helperCreateTask(t, env, "gate/test")

	r := helperRoutine(t, gateStop)

	res, err := HandleAutoAdvance("gate/test", r, "plan", time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "review", res.NextStep, "should land on the gate step")
	require.False(t, res.Dispatch, "gate steps never auto-dispatch")
}

// TestHandleAutoAdvance_TerminalEmitsRoutineSurfacedEvent: landing on
// a default-surfaced terminal appends a routine.surfaced history event
// the unread substrate can watch.
func TestHandleAutoAdvance_TerminalEmitsRoutineSurfacedEvent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "claude", "opus")
	helperCreateTask(t, env, "term/event")

	r := helperRoutine(t, linearTwoStep)

	ts := time.Now().UTC()
	_, err := HandleAutoAdvance("term/event", r, "plan", ts)
	require.NoError(t, err)

	events, err := history.Read("term/event", history.ReadOptions{})
	require.NoError(t, err)
	var sawSurfaced bool
	for _, ev := range events {
		if ev.Type == "routine.surfaced" {
			sawSurfaced = true
			var d struct {
				Step string `json:"step"`
				Kind string `json:"kind"`
			}
			require.NoError(t, json.Unmarshal(ev.Data, &d))
			require.Equal(t, "done", d.Step)
			require.Equal(t, "terminal", d.Kind)
		}
	}
	require.True(t, sawSurfaced, "routine.surfaced event must be emitted on entry to a surfaced terminal")
}

// surface:false suppresses the routine.surfaced event.
func TestHandleAutoAdvance_TerminalSurfaceFalseSuppressesEvent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "claude", "opus")
	helperCreateTask(t, env, "term/silent")

	const silentTerm = `name: term-silent
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: cancelled
    kind: terminal
    surface: false
`
	r := helperRoutine(t, silentTerm)

	ts := time.Now().UTC()
	_, err := HandleAutoAdvance("term/silent", r, "plan", ts)
	require.NoError(t, err)

	events, err := history.Read("term/silent", history.ReadOptions{})
	require.NoError(t, err)
	for _, ev := range events {
		require.NotEqual(t, "routine.surfaced", ev.Type,
			"routine.surfaced must NOT be emitted when terminal has surface: false")
	}
}

// Gates share the same surface-event semantic.
func TestHandleAutoAdvance_GateEmitsRoutineSurfacedEvent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "claude", "opus")
	helperCreateTask(t, env, "gate/event")

	const gateRoutine = `name: gate-surface
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    kind: gate
    options:
      - { name: approve, next: done }
  - id: done
    kind: terminal
`
	r := helperRoutine(t, gateRoutine)

	ts := time.Now().UTC()
	_, err := HandleAutoAdvance("gate/event", r, "plan", ts)
	require.NoError(t, err)

	events, err := history.Read("gate/event", history.ReadOptions{})
	require.NoError(t, err)
	var sawSurfaced bool
	for _, ev := range events {
		if ev.Type == "routine.surfaced" {
			sawSurfaced = true
			var d struct {
				Step string `json:"step"`
				Kind string `json:"kind"`
			}
			require.NoError(t, json.Unmarshal(ev.Data, &d))
			require.Equal(t, "review", d.Step)
			require.Equal(t, "gate", d.Kind)
		}
	}
	require.True(t, sawSurfaced, "routine.surfaced event must be emitted on entry to a surfaced gate")
}

// Gate with surface: false suppresses the event.
func TestHandleAutoAdvance_GateSurfaceFalseSuppressesEvent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "claude", "opus")
	helperCreateTask(t, env, "gate/silent")

	const silentGate = `name: gate-silent
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    kind: gate
    surface: false
    options:
      - { name: approve, next: done }
  - id: done
    kind: terminal
`
	r := helperRoutine(t, silentGate)

	ts := time.Now().UTC()
	_, err := HandleAutoAdvance("gate/silent", r, "plan", ts)
	require.NoError(t, err)

	events, err := history.Read("gate/silent", history.ReadOptions{})
	require.NoError(t, err)
	for _, ev := range events {
		require.NotEqual(t, "routine.surfaced", ev.Type,
			"routine.surfaced must NOT be emitted when gate has surface: false")
	}
}

// TestHandleAutoAdvance_CrossAdapterSwapClearsSession mirrors
// TestSend_AutoAdvanceSwapsAdapterAndClearsSession but at the runner level.
func TestHandleAutoAdvance_CrossAdapterSwapClearsSession(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	// Agent "planner" pins adapter=claude; "swapper" pins adapter=codex.
	mkAgent(t, env, "planner", "claude", "m1")
	mkAgent(t, env, "swapper", "codex", "m2")

	const yaml = `name: swap
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: swap
    agent: swapper
    advance: auto
  - id: done
    kind: terminal
`
	r := helperRoutine(t, yaml)
	helperCreateTask(t, env, "swap/task")

	// Pretend we already have a session pinned to claude.
	st := &task.State{SessionID: "sess-1", Adapter: "claude"}
	require.NoError(t, st.Save("swap/task"))

	// And task adapter is claude.
	tk, err := task.Load("swap/task")
	require.NoError(t, err)
	tk.Adapter = "claude"
	tk.Model = "m1"
	require.NoError(t, tk.Save())

	res, err := HandleAutoAdvance("swap/task", r, "plan", time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "swap", res.NextStep)

	st2, err := task.LoadState("swap/task")
	require.NoError(t, err)
	require.Empty(t, st2.SessionID, "session must be cleared on cross-adapter swap")
	require.Equal(t, "codex", st2.Adapter, "state adapter must update to codex")

	tk2, err := task.Load("swap/task")
	require.NoError(t, err)
	require.Equal(t, "codex", tk2.Adapter, "TASK.md adapter must update to codex")
	require.Equal(t, "m2", tk2.Model)
}

// TestHandleAutoAdvance_AgentStepDispatches_PlainStepDoesNot: an agent
// step entry auto-dispatches; a step with no agent and no worker_instructions does not.
func TestHandleAutoAdvance_AgentStepDispatches_PlainStepDoesNot(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "explorer", "claude", "sonnet")
	helperCreateTask(t, env, "plain/task")

	r := helperRoutine(t, plainStep)

	res, err := HandleAutoAdvance("plain/task", r, "explore", time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "impl", res.NextStep)
	require.False(t, res.Dispatch, "step with no agent and no worker_instructions must NOT auto-dispatch")
}

// TestHandleAutoAdvance_NonAutoStepStops: advance != "auto" → no transition.
func TestHandleAutoAdvance_NonAutoStepStops(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "claude", "opus")
	helperCreateTask(t, env, "nonauto/task")

	const yaml = `name: nonauto
steps:
  - id: plan
    agent: planner
  - id: done
    kind: terminal
`
	r := helperRoutine(t, yaml)

	res, err := HandleAutoAdvance("nonauto/task", r, "plan", time.Now().UTC())
	require.NoError(t, err)
	require.Empty(t, res.NextStep, "advance!=auto must not advance")
}

// TestReadArtifactBool_MalformedFrontmatter: unterminated frontmatter
// surfaces as an error.
func TestReadArtifactBool_MalformedFrontmatter(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	helperCreateTask(t, env, "bad/fm")

	dir := task.Dir("bad/fm")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "PLAN.md"),
		[]byte("---\nfield: true\n(no closing fence)\n"), 0o644))

	_, err := readArtifactBool(dir, "PLAN.md", "field")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unclosed")
}

// TestReadArtifactBool_NoFrontmatter: artifact without frontmatter → false, no error.
func TestReadArtifactBool_NoFrontmatter(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	helperCreateTask(t, env, "nofm/task")

	dir := task.Dir("nofm/task")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "PLAN.md"),
		[]byte("Plain markdown without frontmatter.\n"), 0o644))

	v, err := readArtifactBool(dir, "PLAN.md", "any_field")
	require.NoError(t, err)
	require.False(t, v)
}

// dispatch2 is a fixture whose advance:auto step's declaration-order successor
// is a regular step carrying worker_instructions (so it dispatches).
const dispatch2 = `name: dispatch2
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: impl
    worker_instructions: implement it
  - id: done
    kind: terminal
`

// dispatchGhostAgent is a fixture whose advance:auto step's successor binds an
// agent that does not resolve — the dispatch condition holds but the agent load
// fails, so WouldAutoDispatch must surface the error (matching HandleAutoAdvance).
const dispatchGhostAgent = `name: dispatchghost
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: impl
    agent: ghost-agent
  - id: done
    kind: terminal
`

// TestWouldAutoDispatch covers the read-only predicate wait uses to close the
// auto-advance race window. It must agree with HandleAutoAdvance's dispatch
// decision without performing any transition.
func TestWouldAutoDispatch(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	t.Run("auto step with dispatchable regular next", func(t *testing.T) {
		helperCreateTask(t, env, "wad/true")
		got, err := WouldAutoDispatch("wad/true", helperRoutine(t, dispatch2), "plan")
		require.NoError(t, err)
		require.True(t, got)
	})

	t.Run("non-auto current step", func(t *testing.T) {
		helperCreateTask(t, env, "wad/nonauto")
		got, err := WouldAutoDispatch("wad/nonauto", helperRoutine(t, dispatch2), "impl")
		require.NoError(t, err)
		require.False(t, got)
	})

	t.Run("terminal current step", func(t *testing.T) {
		helperCreateTask(t, env, "wad/term")
		got, err := WouldAutoDispatch("wad/term", helperRoutine(t, dispatch2), "done")
		require.NoError(t, err)
		require.False(t, got)
	})

	t.Run("missing current step", func(t *testing.T) {
		helperCreateTask(t, env, "wad/missing")
		got, err := WouldAutoDispatch("wad/missing", helperRoutine(t, dispatch2), "nope")
		require.NoError(t, err)
		require.False(t, got)
	})

	t.Run("next is a gate", func(t *testing.T) {
		helperCreateTask(t, env, "wad/gate")
		got, err := WouldAutoDispatch("wad/gate", helperRoutine(t, gateStop), "plan")
		require.NoError(t, err)
		require.False(t, got)
	})

	t.Run("next is a terminal", func(t *testing.T) {
		helperCreateTask(t, env, "wad/nextterm")
		got, err := WouldAutoDispatch("wad/nextterm", helperRoutine(t, linearTwoStep), "plan")
		require.NoError(t, err)
		require.False(t, got)
	})

	t.Run("next regular without agent or instructions", func(t *testing.T) {
		helperCreateTask(t, env, "wad/plain")
		got, err := WouldAutoDispatch("wad/plain", helperRoutine(t, plainStep), "explore")
		require.NoError(t, err)
		require.False(t, got)
	})

	t.Run("nil routine", func(t *testing.T) {
		got, err := WouldAutoDispatch("wad/nilr", nil, "plan")
		require.NoError(t, err)
		require.False(t, got)
	})

	t.Run("branch-selected next follows the branch", func(t *testing.T) {
		helperCreateTask(t, env, "wad/branch")
		// The loopback lands on the agent step, so its agent must resolve for
		// the dispatch predicate to hold (mirrors HandleAutoAdvance's gate).
		mkAgent(t, env, "planner", "claude", "m")
		helperWriteArtifact(t, "wad/branch", "PLAN.md",
			map[string]any{"needs_rework": true}, "")
		got, err := WouldAutoDispatch("wad/branch", helperRoutine(t, branchLoopback), "plan")
		require.NoError(t, err)
		require.True(t, got, "branch loops back to the agent step, which dispatches")
	})

	t.Run("malformed produces frontmatter propagates the error", func(t *testing.T) {
		helperCreateTask(t, env, "wad/malformed")
		require.NoError(t, os.WriteFile(
			filepath.Join(task.Dir("wad/malformed"), "PLAN.md"),
			[]byte("---\nneeds_rework: true"), 0o644))
		_, err := WouldAutoDispatch("wad/malformed", helperRoutine(t, branchLoopback), "plan")
		require.Error(t, err)
	})

	// A dispatchable next step whose agent cannot be loaded must surface the
	// error, matching HandleAutoAdvance's ResolveStepAgent gate — otherwise wait
	// would hold the task "pending" for the whole guard window and mislabel it
	// "supervisor died mid-advance".
	t.Run("next binds an unloadable agent propagates the error", func(t *testing.T) {
		helperCreateTask(t, env, "wad/ghost")
		_, err := WouldAutoDispatch("wad/ghost", helperRoutine(t, dispatchGhostAgent), "plan")
		require.Error(t, err)
	})
}
