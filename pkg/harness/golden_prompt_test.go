package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestBuildPrompt_WorkspaceBlock(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/workspace",
		Title:       "Workspace block",
		BaseBranch:  "develop",
		Description: "Per-task description.",
	}
	require.NoError(t, tk.Save())

	got, err := BuildPrompt(tk, "/tmp/ws-block", false, "", "Implement.", nil)
	require.NoError(t, err)

	// Block is present and names the workspace, base branch, and task branch.
	require.Contains(t, got, "## Workspace\n")
	require.Contains(t, got, "Your working directory is `/tmp/ws-block`.")
	require.Contains(t, got, "git worktree of `develop` on branch `prompt/workspace`")
	// Negative constraint must be explicit — this is the load-bearing line.
	require.Contains(t, got, "Never use absolute paths to other clones of this project")
	require.Contains(t, got, "git rev-parse --show-toplevel")
	// Ordering: Workspace before Description.
	wsIdx := strings.Index(got, "## Workspace")
	descIdx := strings.Index(got, "## Description")
	require.Greater(t, descIdx, wsIdx, "## Workspace should appear before ## Description")
}

func TestBuildPrompt_NoWorkspaceBlockWhenEmpty(t *testing.T) {
	// Empty workspace string is allowed for non-dispatch contexts (tests
	// building prompts in isolation, future ad-hoc callers). The block is
	// skipped — the worker would have no path to pin to anyway.
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/no-workspace",
		Title:       "No workspace",
		BaseBranch:  "main",
		Description: "Test.",
	}
	require.NoError(t, tk.Save())

	got, err := BuildPrompt(tk, "", false, "", "Implement.", nil)
	require.NoError(t, err)
	require.NotContains(t, got, "## Workspace", "## Workspace must be omitted when workspace path is empty")
}

func TestGolden_BuildPrompt_BasicTask(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/basic",
		Title:       "Basic prompt task",
		BaseBranch:  "main",
		Description: "Do the basic thing.",
	}
	require.NoError(t, tk.Save())

	got, err := BuildPrompt(tk, "/tmp/ws", false, "", "Please implement it.", nil)
	require.NoError(t, err)
	testutil.AssertGolden(t, "testdata/prompt/basic.txt", got)
}

func TestGolden_BuildPrompt_ContextSameWorkspace(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/context-same",
		Title:       "Context prompt (same workspace)",
		BaseBranch:  "main",
		FollowUp:    "ctx/task",
		Description: "Continue the previous work.",
	}
	require.NoError(t, tk.Save())

	got, err := BuildPrompt(tk, "/tmp/ws", true, "", "Continue.", nil)
	require.NoError(t, err)
	testutil.AssertGolden(t, "testdata/prompt/context_same_workspace.txt", got)
}

func TestGolden_BuildPrompt_ContextNewWorkspace(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/context-new",
		Title:       "Context prompt (new workspace)",
		BaseBranch:  "main",
		FollowUp:    "ctx/task",
		Description: "Continue the previous work.",
	}
	require.NoError(t, tk.Save())

	got, err := BuildPrompt(tk, "/tmp/ws", false, "", "Continue.", nil)
	require.NoError(t, err)
	testutil.AssertGolden(t, "testdata/prompt/context_new_workspace.txt", got)
}

func TestGolden_BuildPrompt_WithExtraFiles(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/extra-files",
		Title:       "Extra files prompt task",
		BaseBranch:  "main",
		Description: "Use the extra files in the task folder.",
	}
	require.NoError(t, tk.Save())

	taskDir := task.Dir(tk.Name)
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "PLAN.md"), []byte("# Plan\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "PROGRESS.json"), []byte("[]\n"), 0o644))

	got, err := BuildPrompt(tk, "/tmp/ws", false, "", "Follow PLAN.md and update PROGRESS.json.", nil)
	require.NoError(t, err)
	testutil.AssertGolden(t, "testdata/prompt/with_extra_files.txt", got)
}

func TestBuildPrompt_InjectsAgent(t *testing.T) {
	// When Task.Agent is set, a ## Agent block must appear before
	// ## Description, carrying the agent's resolved prompt text.
	env := testutil.NewTestEnv(t, 0)

	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(
		`adapter: codex
model: gpt-5.5
prompt:
  text: |
    You are the planner. Read the spec, write PLAN.md.
`), 0o644))

	tk := &task.Task{
		Name:        "prompt/with-agent",
		Title:       "Agent injection",
		BaseBranch:  "main",
		Agent:       "planner",
		Description: "Per-task description.",
	}
	require.NoError(t, tk.Save())

	got, err := BuildPrompt(tk, "/tmp/ws", false, "", "Implement.", nil)
	require.NoError(t, err)

	require.Contains(t, got, "## Agent\n", "## Agent header must be present")
	require.Contains(t, got, "You are the planner.")

	// Ordering: Agent → Description.
	agentIdx := strings.Index(got, "## Agent")
	descIdx := strings.Index(got, "## Description")
	require.Greater(t, descIdx, agentIdx, "## Description must follow ## Agent")
}

func TestBuildPrompt_NoAgentBlockWhenUnset(t *testing.T) {
	// Backward-compat: a task without Task.Agent must produce no
	// ## Agent block. Existing golden snapshots verify byte-identical
	// output; this test is the local header-absent check.
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/no-agent",
		Title:       "No agent",
		BaseBranch:  "main",
		Description: "Per-task description.",
	}
	require.NoError(t, tk.Save())

	got, err := BuildPrompt(tk, "/tmp/ws", false, "", "Implement.", nil)
	require.NoError(t, err)
	require.NotContains(t, got, "## Agent", "## Agent must be omitted when Task.Agent is empty")
}

// --- Routine-aware prompt assembly -------------------------------------------

func TestBuildPrompt_RoutineDefaultPromptReplacesWorkerMD(t *testing.T) {
	// When t.Routine is set, the `## Project` block must source from
	// routine.default_prompt — even if WORKER.md is present, the routine
	// path ignores it. This is the load-bearing decision documented in
	// docs/dev/_audit-skill-workflow-primitives.md.
	env := testutil.NewTestEnv(t, 0)
	require.NoError(t, os.WriteFile(filepath.Join(env.RootDir, ".subtask", "WORKER.md"),
		[]byte("Should be ignored for routine tasks."), 0o644))

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "flow.yaml"), []byte(
		`name: flow
default_prompt:
  text: Routine project brief — be terse.
steps:
  - id: plan
    advance: auto
  - id: done
    kind: terminal
`), 0o644))

	tk := &task.Task{
		Name:        "rt/project",
		Title:       "Routine project block",
		BaseBranch:  "main",
		Routine:     "flow",
		Description: "Per-task description.",
	}
	require.NoError(t, tk.Save())

	got, err := BuildPrompt(tk, "/tmp/ws", false, "", "Implement.", nil)
	require.NoError(t, err)
	require.Contains(t, got, "## Project\n", "## Project header must be present")
	require.Contains(t, got, "Routine project brief", "default_prompt body must appear")
	require.NotContains(t, got, "Should be ignored", "WORKER.md must NOT leak into routine prompts")
}

func TestBuildPrompt_RoutineNoDefaultPromptNoProjectBlock(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	// WORKER.md exists but must be ignored — routine path skips it.
	require.NoError(t, os.WriteFile(filepath.Join(env.RootDir, ".subtask", "WORKER.md"),
		[]byte("Should be ignored."), 0o644))

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "noProj.yaml"), []byte(
		`name: noProj
steps:
  - id: plan
    advance: auto
  - id: done
    kind: terminal
`), 0o644))

	tk := &task.Task{
		Name:        "rt/no-project",
		Title:       "Routine without default_prompt",
		BaseBranch:  "main",
		Routine:     "noProj",
		Description: "Per-task description.",
	}
	require.NoError(t, tk.Save())

	got, err := BuildPrompt(tk, "/tmp/ws", false, "", "Implement.", nil)
	require.NoError(t, err)
	require.NotContains(t, got, "## Project", "no ## Project block when routine omits default_prompt")
}

func TestBuildPrompt_RoutineAgentPerStep(t *testing.T) {
	// Routine tasks pick the agent from the current step (not t.Agent).
	env := testutil.NewTestEnv(t, 0)

	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(
		`adapter: codex
model: gpt-5
prompt:
  text: |
    You are the planner. Read the spec, write PLAN.md.
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "reviewer.yaml"), []byte(
		`adapter: codex
model: gpt-5
prompt:
  text: |
    You are the reviewer. Inspect; do not modify files.
`), 0o644))

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "perStep.yaml"), []byte(
		`name: perStep
steps:
  - id: plan
    agent: planner
    advance: auto
  - id: review
    agent: reviewer
    advance: auto
  - id: done
    kind: terminal
`), 0o644))

	tk := &task.Task{
		Name:        "rt/per-step",
		Title:       "Routine per-step agent",
		BaseBranch:  "main",
		Routine:     "perStep",
		Description: "Per-task description.",
	}
	require.NoError(t, tk.Save())

	gotPlan, err := BuildPrompt(tk, "/tmp/ws", false, "plan", "Plan.", nil)
	require.NoError(t, err)
	require.Contains(t, gotPlan, "You are the planner.", "plan step should inject the planner agent's prompt")
	require.NotContains(t, gotPlan, "You are the reviewer.")

	gotReview, err := BuildPrompt(tk, "/tmp/ws", false, "review", "Review.", nil)
	require.NoError(t, err)
	require.Contains(t, gotReview, "You are the reviewer.")
	require.NotContains(t, gotReview, "You are the planner.")
}

func TestBuildPrompt_RoutineStepWithNoAgentHasNoAgentBlock(t *testing.T) {
	// A routine step with no agent: field must produce NO `## Agent`
	// block — t.Agent is not a fallback for routine tasks.
	env := testutil.NewTestEnv(t, 0)
	_ = env

	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, "noAgent.yaml"), []byte(
		`name: noAgent
steps:
  - id: impl
    advance: auto
  - id: done
    kind: terminal
`), 0o644))

	tk := &task.Task{
		Name:        "rt/no-agent-step",
		Title:       "Routine step without agent",
		BaseBranch:  "main",
		Routine:     "noAgent",
		Agent:       "should-not-load", // routine ignores t.Agent
		Description: "Per-task description.",
	}
	require.NoError(t, tk.Save())

	got, err := BuildPrompt(tk, "/tmp/ws", false, "impl", "Build.", nil)
	require.NoError(t, err)
	require.NotContains(t, got, "## Agent", "step with no agent: must not emit ## Agent block")
}

func TestBuildPrompt_AgentLoadFailurePropagates(t *testing.T) {
	// Missing agent file → BuildPrompt returns an actionable error
	// citing the expected path.
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/missing-agent",
		Title:       "Missing agent",
		BaseBranch:  "main",
		Agent:       "ghost",
		Description: "Test.",
	}
	require.NoError(t, tk.Save())

	_, err := BuildPrompt(tk, "/tmp/ws", false, "", "Implement.", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), ".subtask/agents/ghost.yaml")
}

// --- 3a: consumes: → ## Inputs ------------------------------------------------

func writeRoutine(t *testing.T, env *testutil.TestEnv, name, body string) {
	t.Helper()
	routinesDir := filepath.Join(env.RootDir, ".subtask", "routines")
	require.NoError(t, os.MkdirAll(routinesDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(routinesDir, name+".yaml"), []byte(body), 0o644))
}

func TestBuildPrompt_ConsumesInjectsInputs(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	writeRoutine(t, env, "consumes", `name: consumes
steps:
  - id: impl
    consumes: [PLAN.md, notes/spec.md]
    worker_instructions: Implement per PLAN.md.
  - id: done
    kind: terminal
`)

	tk := &task.Task{
		Name:        "rt/consumes-unit",
		Title:       "Consumes inputs",
		BaseBranch:  "main",
		Routine:     "consumes",
		Description: "Per-task description.",
	}
	require.NoError(t, tk.Save())
	// PLAN.md exists; notes/spec.md deliberately absent (missing-marked).
	require.NoError(t, os.WriteFile(filepath.Join(task.Dir(tk.Name), "PLAN.md"), []byte("# Plan\n"), 0o644))

	got, err := BuildPrompt(tk, "/tmp/ws", false, "impl", "Go.", nil)
	require.NoError(t, err)

	require.Contains(t, got, "## Inputs")
	require.Contains(t, got, ".subtask/tasks/rt--consumes-unit/PLAN.md")
	require.Contains(t, got, "notes/spec.md (missing — expected input not found)")

	// Ordering: ## Stage < ## Inputs < separator.
	stageIdx := strings.Index(got, "## Stage")
	inputsIdx := strings.Index(got, "## Inputs")
	sepIdx := strings.Index(got, "--------------------")
	require.Greater(t, inputsIdx, stageIdx, "## Inputs must follow ## Stage")
	require.Greater(t, sepIdx, inputsIdx, "## Inputs must precede the separator")
}

func TestBuildPrompt_NoInputsWhenNoConsumes(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	writeRoutine(t, env, "noConsume", `name: noConsume
steps:
  - id: impl
    worker_instructions: Just do it.
  - id: done
    kind: terminal
`)

	tk := &task.Task{
		Name:       "rt/no-consume",
		Title:      "No consume",
		BaseBranch: "main",
		Routine:    "noConsume",
	}
	require.NoError(t, tk.Save())

	got, err := BuildPrompt(tk, "/tmp/ws", false, "impl", "Go.", nil)
	require.NoError(t, err)
	require.NotContains(t, got, "## Inputs")
}

func TestBuildPrompt_NoInputsForNonRoutine(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/plain-no-inputs",
		Title:       "Plain",
		BaseBranch:  "main",
		Description: "Plain task.",
	}
	require.NoError(t, tk.Save())

	got, err := BuildPrompt(tk, "/tmp/ws", false, "", "Go.", nil)
	require.NoError(t, err)
	require.NotContains(t, got, "## Inputs")
}

func TestGolden_BuildPrompt_Consumes(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	writeRoutine(t, env, "consumesg", `name: consumesg
steps:
  - id: impl
    consumes: [PLAN.md, notes/spec.md]
    worker_instructions: Implement per PLAN.md.
  - id: done
    kind: terminal
`)

	tk := &task.Task{
		Name:        "rt/consumes",
		Title:       "Consumes golden",
		BaseBranch:  "main",
		Routine:     "consumesg",
		Description: "Per-task description.",
	}
	require.NoError(t, tk.Save())
	require.NoError(t, os.WriteFile(filepath.Join(task.Dir(tk.Name), "PLAN.md"), []byte("# Plan\n"), 0o644))

	got, err := BuildPrompt(tk, "/tmp/ws", false, "impl", "Go.", nil)
	require.NoError(t, err)
	testutil.AssertGolden(t, "testdata/prompt/consumes.txt", got)
}

func TestBuildPrompt_ConsumesWithoutStageBlock(t *testing.T) {
	// A regular step may declare consumes: with neither worker_instructions
	// nor worker_context (dispatched via `stage <task> <step> "<prompt>"` or a
	// direct `send` — auto-advance can't reach it since that requires
	// agent:/worker_instructions:). ## Inputs is keyed on len(consumes) > 0
	// alone, independent of ## Stage, so it must still render even though
	// ## Stage: (which requires wi != "" || wc != "") does not.
	env := testutil.NewTestEnv(t, 0)
	writeRoutine(t, env, "consumesNoStage", `name: consumesNoStage
steps:
  - id: impl
    consumes: [PLAN.md]
  - id: done
    kind: terminal
`)

	tk := &task.Task{
		Name:       "rt/consumes-no-stage",
		Title:      "Consumes without stage block",
		BaseBranch: "main",
		Routine:    "consumesNoStage",
	}
	require.NoError(t, tk.Save())
	require.NoError(t, os.WriteFile(filepath.Join(task.Dir(tk.Name), "PLAN.md"), []byte("# Plan\n"), 0o644))

	got, err := BuildPrompt(tk, "/tmp/ws", false, "impl", "Go.", nil)
	require.NoError(t, err)

	require.Contains(t, got, "## Inputs")
	require.NotContains(t, got, "## Stage:")
}

func TestBuildPrompt_ConsumesDirectoryEntry(t *testing.T) {
	// A consumes: entry that resolves to a directory (not a file) renders
	// present, annotated `(directory)` — read semantics are left to the worker.
	env := testutil.NewTestEnv(t, 0)
	writeRoutine(t, env, "consumesDir", `name: consumesDir
steps:
  - id: impl
    consumes: [notes]
    worker_instructions: Read the notes directory.
  - id: done
    kind: terminal
`)

	tk := &task.Task{
		Name:       "rt/consumes-dir",
		Title:      "Consumes directory entry",
		BaseBranch: "main",
		Routine:    "consumesDir",
	}
	require.NoError(t, tk.Save())
	require.NoError(t, os.MkdirAll(filepath.Join(task.Dir(tk.Name), "notes"), 0o755))

	got, err := BuildPrompt(tk, "/tmp/ws", false, "impl", "Go.", nil)
	require.NoError(t, err)

	require.Contains(t, got, "## Inputs")
	require.Contains(t, got, ".subtask/tasks/rt--consumes-dir/notes (directory)")
}

// --- 3b: ## Parent Context ----------------------------------------------------

func TestBuildPrompt_ParentContextInjected(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	parent := &task.Task{
		Name:        "parent/x",
		Title:       "Parent",
		BaseBranch:  "main",
		Description: "Parent work.",
	}
	require.NoError(t, parent.Save())
	pdir := task.Dir(parent.Name)
	require.NoError(t, os.WriteFile(filepath.Join(pdir, "PLAN.md"), []byte("# Plan\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(pdir, "PROGRESS.json"), []byte("[]\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(pdir, "reviews"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(pdir, "reviews", "r.md"), []byte("# Review\n"), 0o644))
	data, _ := json.Marshal(map[string]any{"name": "review", "path": "reviews/r.md", "kind": "review"})
	require.NoError(t, history.Append(parent.Name, history.Event{Type: "artifact.produced", Data: data}))

	child := &task.Task{
		Name:        "child/y",
		Title:       "Child",
		BaseBranch:  "main",
		FollowUp:    "parent/x",
		Description: "Child work.",
	}
	require.NoError(t, child.Save())

	got, err := BuildPrompt(child, "/tmp/ws", false, "", "Go.", nil)
	require.NoError(t, err)

	require.Contains(t, got, "## Parent Context")
	require.Contains(t, got, "follow-up from parent/x")
	require.Contains(t, got, "TASK.md:")
	require.Contains(t, got, "PLAN.md:")
	require.Contains(t, got, "PROGRESS.json:")
	require.Contains(t, got, "reviews/r.md")

	// Every listed artifact path is absolute (into the lead repo).
	for line := range strings.SplitSeq(got, "\n") {
		if strings.HasPrefix(line, "- ") && strings.Contains(line, ".subtask/tasks/parent--x/") {
			require.Contains(t, line, env.RootDir, "parent artifact path must be absolute: %q", line)
		}
	}

	// Ordering: ## Description < ## Parent Context.
	descIdx := strings.Index(got, "## Description")
	pcIdx := strings.Index(got, "## Parent Context")
	require.Greater(t, pcIdx, descIdx, "## Parent Context must follow ## Description")
}

func TestBuildPrompt_NoParentContextWhenParentMissing(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	child := &task.Task{
		Name:        "child/ghost",
		Title:       "Child",
		BaseBranch:  "main",
		FollowUp:    "ghost/x", // no such parent folder
		Description: "Child work.",
	}
	require.NoError(t, child.Save())

	got, err := BuildPrompt(child, "/tmp/ws", false, "", "Go.", nil)
	require.NoError(t, err)
	require.NotContains(t, got, "## Parent Context",
		"a follow-up to a nonexistent parent must render no ## Parent Context (this is what keeps the context_* goldens valid)")
}

func TestBuildPrompt_ParentContextSkipsMissingFiles(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	parent := &task.Task{
		Name:       "parent/only-task",
		Title:      "Parent",
		BaseBranch: "main",
	}
	require.NoError(t, parent.Save()) // only TASK.md on disk, no PLAN/PROGRESS

	child := &task.Task{
		Name:       "child/z",
		Title:      "Child",
		BaseBranch: "main",
		FollowUp:   "parent/only-task",
	}
	require.NoError(t, child.Save())

	got, err := BuildPrompt(child, "/tmp/ws", false, "", "Go.", nil)
	require.NoError(t, err)
	require.Contains(t, got, "## Parent Context")
	require.Contains(t, got, "TASK.md:")
	require.NotContains(t, got, "PLAN.md:")
	require.NotContains(t, got, "PROGRESS.json:")
}

func TestBuildPrompt_ParentContextDedupesProgressJSON(t *testing.T) {
	// If a step's produces: is literally "PROGRESS.json" (already emitted as
	// an artifact.produced event) and the file exists on disk, the
	// task.Artifacts() loop in renderParentContext already lists it — the
	// explicit PROGRESS.json fallback append must be skipped so it isn't
	// listed twice.
	_ = testutil.NewTestEnv(t, 0)

	parent := &task.Task{
		Name:       "parent/dedup",
		Title:      "Parent dedup",
		BaseBranch: "main",
	}
	require.NoError(t, parent.Save())
	pdir := task.Dir(parent.Name)
	require.NoError(t, os.WriteFile(filepath.Join(pdir, "PROGRESS.json"), []byte("[]\n"), 0o644))
	data, _ := json.Marshal(map[string]any{"name": "PROGRESS.json", "path": "PROGRESS.json", "kind": "impl"})
	require.NoError(t, history.Append(parent.Name, history.Event{Type: "artifact.produced", Data: data}))

	child := &task.Task{
		Name:       "child/dedup",
		Title:      "Child",
		BaseBranch: "main",
		FollowUp:   "parent/dedup",
	}
	require.NoError(t, child.Save())

	got, err := BuildPrompt(child, "/tmp/ws", false, "", "Go.", nil)
	require.NoError(t, err)
	require.Contains(t, got, "## Parent Context")

	var progressLines int
	for line := range strings.SplitSeq(got, "\n") {
		if strings.HasPrefix(line, "- ") && strings.Contains(line, "PROGRESS.json") {
			progressLines++
		}
	}
	require.Equal(t, 1, progressLines, "PROGRESS.json must appear as exactly one line in ## Parent Context, got %d:\n%s", progressLines, got)
}
