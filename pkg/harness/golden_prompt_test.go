package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
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
