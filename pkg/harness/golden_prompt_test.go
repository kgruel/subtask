package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workflow"
)

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
