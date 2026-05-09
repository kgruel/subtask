package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workflow"
)

func TestBuildPrompt_WorkspaceBlock(t *testing.T) {
	// Drop a WORKER.md so we can also verify ordering: ## Workspace must
	// come before ## Project (the WORKER.md section). The Workspace block
	// is a stronger constraint about *where* to operate; it should land in
	// the worker's context before per-project conventions.
	env := testutil.NewTestEnv(t, 0)
	require.NoError(t, os.WriteFile(filepath.Join(env.RootDir, ".subtask", "WORKER.md"), []byte("Project rule."), 0o644))

	tk := &task.Task{
		Name:        "prompt/workspace",
		Title:       "Workspace block",
		BaseBranch:  "develop",
		Description: "Per-task description.",
	}
	require.NoError(t, tk.Save())

	got := BuildPrompt(tk, "/tmp/ws-block", false, "", "Implement.", nil)

	// Block is present and names the workspace, base branch, and task branch.
	require.Contains(t, got, "## Workspace\n")
	require.Contains(t, got, "Your working directory is `/tmp/ws-block`.")
	require.Contains(t, got, "git worktree of `develop` on branch `prompt/workspace`")
	// Negative constraint must be explicit — this is the load-bearing line.
	require.Contains(t, got, "Never use absolute paths to other clones of this project")
	require.Contains(t, got, "git rev-parse --show-toplevel")
	// Ordering: Workspace before Project before Description.
	wsIdx := strings.Index(got, "## Workspace")
	projIdx := strings.Index(got, "## Project")
	descIdx := strings.Index(got, "## Description")
	require.Greater(t, projIdx, wsIdx, "## Workspace should appear before ## Project")
	require.Greater(t, descIdx, projIdx, "## Project should appear before ## Description")
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

	got := BuildPrompt(tk, "", false, "", "Implement.", nil)
	require.NotContains(t, got, "## Workspace", "## Workspace must be omitted when workspace path is empty")
}

func TestBuildPrompt_InjectsWorkerMD(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	// Drop a project-wide WORKER.md into .subtask/.
	workerMD := "Always regenerate snapshots via UV_PYTHON across {3.12,3.13,3.14}.\nProject commit style: imperative subject + short rationale paragraph."
	require.NoError(t, os.WriteFile(filepath.Join(env.RootDir, ".subtask", "WORKER.md"), []byte(workerMD), 0o644))

	tk := &task.Task{
		Name:        "prompt/worker-md",
		Title:       "WORKER.md injection",
		BaseBranch:  "main",
		Description: "Per-task description.",
	}
	require.NoError(t, tk.Save())

	got := BuildPrompt(tk, "/tmp/ws", false, "", "Implement.", nil)

	require.Contains(t, got, "## Project\n", "expected ## Project section header")
	require.Contains(t, got, "Always regenerate snapshots via UV_PYTHON")
	require.Contains(t, got, "Project commit style:")
	// Project section must come before Description so worker reads ambient
	// context first, then per-task brief.
	projectIdx := strings.Index(got, "## Project")
	descIdx := strings.Index(got, "## Description")
	require.Greater(t, descIdx, projectIdx, "## Project should appear before ## Description")
}

func TestBuildPrompt_InjectsWorkerContext(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	const wf = `name: ctx-flow
stages:
  - name: implement
    worker_context: Commit your work when done.
    instructions: Do work.
`
	require.NoError(t, os.MkdirAll(filepath.Join(env.RootDir, ".subtask", "tasks", "ctx--injection"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(env.RootDir, ".subtask", "tasks", "ctx--injection", "WORKFLOW.yaml"), []byte(wf), 0o644))

	tk := &task.Task{
		Name:        "ctx/injection",
		Title:       "worker_context injection",
		BaseBranch:  "main",
		Description: "Test worker_context lands in prompt.",
	}
	require.NoError(t, tk.Save())

	got := BuildPrompt(tk, "/tmp/ws", false, "implement", "Implement.", nil)

	require.Contains(t, got, "## Stage: implement", "stage header should be present")
	require.Contains(t, got, "Commit your work when done.", "worker_context body should be injected")
}

func TestBuildPrompt_NoWorkerMDWhenAbsent(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/no-worker-md",
		Title:       "No WORKER.md",
		BaseBranch:  "main",
		Description: "Per-task description.",
	}
	require.NoError(t, tk.Save())

	got := BuildPrompt(tk, "/tmp/ws", false, "", "Implement.", nil)
	require.NotContains(t, got, "## Project", "## Project section should not appear when WORKER.md is absent")
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

	got := BuildPrompt(tk, "/tmp/ws", false, "", "Please implement it.", nil)
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

	got := BuildPrompt(tk, "/tmp/ws", true, "", "Continue.", nil)
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

	got := BuildPrompt(tk, "/tmp/ws", false, "", "Continue.", nil)
	testutil.AssertGolden(t, "testdata/prompt/context_new_workspace.txt", got)
}

func TestGolden_BuildPrompt_WithWorkflow(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/workflow",
		Title:       "Workflow prompt task",
		BaseBranch:  "main",
		Description: "Do the workflow thing.",
	}
	require.NoError(t, tk.Save())
	require.NoError(t, workflow.CopyToTask("default", tk.Name))

	got := BuildPrompt(tk, "/tmp/ws", false, "", "Implement as described.", nil)
	testutil.AssertGolden(t, "testdata/prompt/with_workflow.txt", got)
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
	require.NoError(t, workflow.CopyToTask("default", tk.Name))

	taskDir := task.Dir(tk.Name)
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "PLAN.md"), []byte("# Plan\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "PROGRESS.json"), []byte("[]\n"), 0o644))

	got := BuildPrompt(tk, "/tmp/ws", false, "", "Follow PLAN.md and update PROGRESS.json.", nil)
	testutil.AssertGolden(t, "testdata/prompt/with_extra_files.txt", got)
}

const workflowWithStageWorkerInstructions = `name: impl-review
instructions:
    worker: |
        Track progress in PROGRESS.json.
stages:
    - name: implement
      instructions: |
          Worker is implementing.
    - name: review
      instructions: |
          Worker is reviewing.
      worker_instructions: |
          Findings only — do NOT modify files.
          Write your review to REVIEW.md using:
            Critical / Important / Minor / Out-of-scope
`

func TestGolden_BuildPrompt_StageWithWorkerInstructions(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/stage-worker",
		Title:       "Stage worker instructions",
		BaseBranch:  "main",
		Description: "Implement then review.",
	}
	require.NoError(t, tk.Save())
	taskDir := task.Dir(tk.Name)
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "WORKFLOW.yaml"), []byte(workflowWithStageWorkerInstructions), 0o644))

	got := BuildPrompt(tk, "/tmp/ws", false, "review", "Review now.", nil)
	testutil.AssertGolden(t, "testdata/prompt/stage_worker_instructions.txt", got)
}

func TestGolden_BuildPrompt_StageWithoutWorkerInstructions(t *testing.T) {
	// implement stage has no worker_instructions; output must not contain a Stage block.
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/stage-no-worker",
		Title:       "Stage without worker instructions",
		BaseBranch:  "main",
		Description: "Implement.",
	}
	require.NoError(t, tk.Save())
	taskDir := task.Dir(tk.Name)
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "WORKFLOW.yaml"), []byte(workflowWithStageWorkerInstructions), 0o644))

	got := BuildPrompt(tk, "/tmp/ws", false, "implement", "Go.", nil)
	testutil.AssertGolden(t, "testdata/prompt/stage_no_worker_instructions.txt", got)
}

func TestBuildPrompt_UnknownStageDoesNotInject(t *testing.T) {
	// An unknown stage name must not error and must not inject a Stage block.
	_ = testutil.NewTestEnv(t, 0)

	tk := &task.Task{
		Name:        "prompt/stage-unknown",
		Title:       "Unknown stage name",
		BaseBranch:  "main",
		Description: "Test.",
	}
	require.NoError(t, tk.Save())
	taskDir := task.Dir(tk.Name)
	require.NoError(t, os.WriteFile(filepath.Join(taskDir, "WORKFLOW.yaml"), []byte(workflowWithStageWorkerInstructions), 0o644))

	got := BuildPrompt(tk, "/tmp/ws", false, "ghost-stage", "Run.", nil)
	require.NotContains(t, got, "## Stage:")
}
