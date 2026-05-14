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
	"github.com/kgruel/subtask/pkg/workspace"
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
      - { name: approve, to: done }
      - { name: revise,  to: plan }
  - id: done
    kind: terminal
`

const presetOnlyStep = `name: preset-only
steps:
  - id: explore
    agent: explorer
    advance: auto
  - id: impl
    preset: opus-high
    advance: auto
  - id: done
    kind: terminal
`

func mkAgent(t *testing.T, env *testutil.TestEnv, name, preset string) {
	t.Helper()
	dir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(
		"preset: "+preset+"\nprompt:\n  text: |\n    You are "+name+".\n"), 0o644))
}

func mkAgentInline(t *testing.T, env *testutil.TestEnv, name, adapter, model string) {
	t.Helper()
	dir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(
		"preset:\n  adapter: "+adapter+"\n  model: "+model+"\nprompt:\n  text: |\n    You are "+name+".\n"), 0o644))
}

func newCfgWithPreset(name, adapter, model string) *workspace.Config {
	return &workspace.Config{
		Adapter: "claude",
		Model:   "test",
		Presets: map[string]workspace.Preset{
			name: {Adapter: adapter, Model: model},
		},
	}
}

// TestHandleAutoAdvance_LinearAdvance: no branches, default-advance to next step.
func TestHandleAutoAdvance_LinearAdvance(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "alt")
	helperCreateTask(t, env, "lin/adv")

	cfg := newCfgWithPreset("alt", "claude", "m2")
	r := helperRoutine(t, linearTwoStep)

	res, err := HandleAutoAdvance("lin/adv", r, "plan", cfg, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "done", res.NextStep, "should default-advance to next in declaration order")
	require.False(t, res.Dispatch, "terminal steps never dispatch")
}

// TestHandleAutoAdvance_BranchTakenWhenFieldTrue: branches with bool=true → branch.to taken.
func TestHandleAutoAdvance_BranchTakenWhenFieldTrue(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "alt")
	helperCreateTask(t, env, "br/true")
	helperWriteArtifact(t, "br/true", "PLAN.md",
		map[string]any{"needs_rework": true},
		"")

	cfg := newCfgWithPreset("alt", "claude", "m2")
	r := helperRoutine(t, branchLoopback)

	res, err := HandleAutoAdvance("br/true", r, "plan", cfg, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "plan", res.NextStep, "branch field=true should loop back to plan")
	require.True(t, res.Dispatch, "agent-driven regular step must auto-dispatch")
}

// TestHandleAutoAdvance_BranchSkippedWhenFieldFalse: field=false → default-advance.
func TestHandleAutoAdvance_BranchSkippedWhenFieldFalse(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "alt")
	helperCreateTask(t, env, "br/false")
	helperWriteArtifact(t, "br/false", "PLAN.md",
		map[string]any{"needs_rework": false},
		"")

	cfg := newCfgWithPreset("alt", "claude", "m2")
	r := helperRoutine(t, branchLoopback)

	res, err := HandleAutoAdvance("br/false", r, "plan", cfg, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "done", res.NextStep, "field=false should fall through to declaration order")
}

// TestHandleAutoAdvance_BranchSkippedWhenFieldAbsent: field missing → default-advance.
func TestHandleAutoAdvance_BranchSkippedWhenFieldAbsent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "alt")
	helperCreateTask(t, env, "br/absent")
	helperWriteArtifact(t, "br/absent", "PLAN.md",
		map[string]any{"other_field": true},
		"")

	cfg := newCfgWithPreset("alt", "claude", "m2")
	r := helperRoutine(t, branchLoopback)

	res, err := HandleAutoAdvance("br/absent", r, "plan", cfg, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "done", res.NextStep)
}

// TestHandleAutoAdvance_BranchSkippedWhenArtifactMissing: file missing → default-advance.
func TestHandleAutoAdvance_BranchSkippedWhenArtifactMissing(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "alt")
	helperCreateTask(t, env, "br/missing")
	// No PLAN.md written.

	cfg := newCfgWithPreset("alt", "claude", "m2")
	r := helperRoutine(t, branchLoopback)

	res, err := HandleAutoAdvance("br/missing", r, "plan", cfg, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "done", res.NextStep)
}

// TestHandleAutoAdvance_TerminalStops: current step terminal → no advance.
func TestHandleAutoAdvance_TerminalStops(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "alt")
	helperCreateTask(t, env, "term/stops")

	cfg := newCfgWithPreset("alt", "claude", "m2")
	r := helperRoutine(t, linearTwoStep)

	res, err := HandleAutoAdvance("term/stops", r, "done", cfg, time.Now().UTC())
	require.NoError(t, err)
	require.Empty(t, res.NextStep, "terminal step must not advance")
}

// TestHandleAutoAdvance_GateNeverDispatches: advancing into a gate stops dispatch.
func TestHandleAutoAdvance_GateNeverDispatches(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "alt")
	helperCreateTask(t, env, "gate/test")

	cfg := newCfgWithPreset("alt", "claude", "m2")
	r := helperRoutine(t, gateStop)

	res, err := HandleAutoAdvance("gate/test", r, "plan", cfg, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "review", res.NextStep, "should land on the gate step")
	require.False(t, res.Dispatch, "gate steps never auto-dispatch")
}

// TestHandleAutoAdvance_TerminalEmitsRoutineSurfacedEvent: landing on
// a default-surfaced terminal appends a routine.surfaced history event
// the unread substrate can watch (gates and terminals share the event
// type since they share the "lead must act" semantic).
func TestHandleAutoAdvance_TerminalEmitsRoutineSurfacedEvent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "alt")
	helperCreateTask(t, env, "term/event")

	cfg := newCfgWithPreset("alt", "claude", "m2")
	r := helperRoutine(t, linearTwoStep)

	ts := time.Now().UTC()
	_, err := HandleAutoAdvance("term/event", r, "plan", cfg, ts)
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

// surface:false suppresses the routine.surfaced event so the routine
// author can mark a terminal as "silent finish" (e.g. cancellation).
func TestHandleAutoAdvance_TerminalSurfaceFalseSuppressesEvent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "alt")
	helperCreateTask(t, env, "term/silent")

	cfg := newCfgWithPreset("alt", "claude", "m2")
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
	_, err := HandleAutoAdvance("term/silent", r, "plan", cfg, ts)
	require.NoError(t, err)

	events, err := history.Read("term/silent", history.ReadOptions{})
	require.NoError(t, err)
	for _, ev := range events {
		require.NotEqual(t, "routine.surfaced", ev.Type,
			"routine.surfaced must NOT be emitted when terminal has surface: false")
	}
}

// Gates share the same surface-event semantic. Auto-advance into a
// default-surfaced gate must emit routine.surfaced so the lead's
// `subtask unread` view sees the handoff.
func TestHandleAutoAdvance_GateEmitsRoutineSurfacedEvent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "alt")
	helperCreateTask(t, env, "gate/event")

	cfg := newCfgWithPreset("alt", "claude", "m2")
	const gateRoutine = `name: gate-surface
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    kind: gate
    options:
      - { name: approve, to: done }
  - id: done
    kind: terminal
`
	r := helperRoutine(t, gateRoutine)

	ts := time.Now().UTC()
	_, err := HandleAutoAdvance("gate/event", r, "plan", cfg, ts)
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

// Gate with surface: false suppresses the event (e.g. a checkpoint
// gate the lead handles in batch).
func TestHandleAutoAdvance_GateSurfaceFalseSuppressesEvent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "alt")
	helperCreateTask(t, env, "gate/silent")

	cfg := newCfgWithPreset("alt", "claude", "m2")
	const silentGate = `name: gate-silent
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    kind: gate
    surface: false
    options:
      - { name: approve, to: done }
  - id: done
    kind: terminal
`
	r := helperRoutine(t, silentGate)

	ts := time.Now().UTC()
	_, err := HandleAutoAdvance("gate/silent", r, "plan", cfg, ts)
	require.NoError(t, err)

	events, err := history.Read("gate/silent", history.ReadOptions{})
	require.NoError(t, err)
	for _, ev := range events {
		require.NotEqual(t, "routine.surfaced", ev.Type,
			"routine.surfaced must NOT be emitted when gate has surface: false")
	}
}

// TestHandleAutoAdvance_CrossAdapterSwapClearsSession mirrors
// TestSend_AutoAdvanceSwapsAdapterAndClearsSession but at the runner
// level — the routine path must use the same adapter-swap + session-clear
// substrate that workflow tasks rely on.
func TestHandleAutoAdvance_CrossAdapterSwapClearsSession(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	// Agent "planner" pins adapter=claude; "swapper" pins adapter=codex.
	mkAgentInline(t, env, "planner", "claude", "m1")
	mkAgentInline(t, env, "swapper", "codex", "m2")

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

	cfg := &workspace.Config{Adapter: "claude", Model: "m1"}
	res, err := HandleAutoAdvance("swap/task", r, "plan", cfg, time.Now().UTC())
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

// TestHandleAutoAdvance_AgentStepDispatchesPresetStepDoesNot: an agent
// step entry auto-dispatches; a preset-only step entry does not.
func TestHandleAutoAdvance_AgentStepDispatchesPresetStepDoesNot(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "explorer", "alt")
	helperCreateTask(t, env, "presetonly/task")

	cfg := newCfgWithPreset("opus-high", "claude", "opus-x")
	cfg.Presets["alt"] = workspace.Preset{Adapter: "claude", Model: "m2"}

	r := helperRoutine(t, presetOnlyStep)

	res, err := HandleAutoAdvance("presetonly/task", r, "explore", cfg, time.Now().UTC())
	require.NoError(t, err)
	require.Equal(t, "impl", res.NextStep)
	require.False(t, res.Dispatch, "preset-only step (no agent, no worker_instructions) must NOT auto-dispatch")
}

// TestHandleAutoAdvance_NonAutoStepStops: advance != "auto" → no transition.
func TestHandleAutoAdvance_NonAutoStepStops(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	mkAgent(t, env, "planner", "alt")
	helperCreateTask(t, env, "nonauto/task")

	const yaml = `name: nonauto
steps:
  - id: plan
    agent: planner
  - id: done
    kind: terminal
`
	cfg := newCfgWithPreset("alt", "claude", "m2")
	r := helperRoutine(t, yaml)

	res, err := HandleAutoAdvance("nonauto/task", r, "plan", cfg, time.Now().UTC())
	require.NoError(t, err)
	require.Empty(t, res.NextStep, "advance!=auto must not advance")
}

// TestReadArtifactBool_MalformedFrontmatter: unterminated frontmatter
// surfaces as an error. (Default-advance only applies to absence, not
// corruption.)
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
