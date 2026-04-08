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

	got := BuildPrompt(tk, "/tmp/ws", false, "Please implement it.", nil)
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

	got := BuildPrompt(tk, "/tmp/ws", true, "Continue.", nil)
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

	got := BuildPrompt(tk, "/tmp/ws", false, "Continue.", nil)
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

	got := BuildPrompt(tk, "/tmp/ws", false, "Implement as described.", nil)
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

	got := BuildPrompt(tk, "/tmp/ws", false, "Follow PLAN.md and update PROGRESS.json.", nil)
	testutil.AssertGolden(t, "testdata/prompt/with_extra_files.txt", got)
}
