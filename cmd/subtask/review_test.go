package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workspace"
)

func TestReviewCmd_Task_PassesBaseBranchAndInstructions(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "review/test"
	env.CreateTask(taskName, "Review test", "main", "Description")

	// First run the task to create a workspace
	sendMock := harness.NewMockHarness().WithResult("Done", "session-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Do it"}).WithHarness(sendMock).Run())

	// Now test review
	reviewMock := harness.NewMockHarness().WithReviewResult("No issues found")

	stdout, stderr, err := captureStdoutStderr(t, (&ReviewCmd{
		Task:   taskName,
		Prompt: "Focus on security",
	}).WithHarness(reviewMock).Run)

	require.NoError(t, err)
	require.Empty(t, stderr)
	assert.Contains(t, stdout, "No issues found")

	// Verify the mock received correct arguments
	require.Len(t, reviewMock.ReviewCalls, 1)
	call := reviewMock.ReviewCalls[0]
	assert.NotEmpty(t, call.CWD)
	assert.Equal(t, "main", call.Target.BaseBranch)
	assert.Equal(t, "Focus on security", call.Instructions)
}

func TestReviewCmd_Task_NoInstructions(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "review/no-instructions"
	env.CreateTask(taskName, "Review test", "main", "Description")

	sendMock := harness.NewMockHarness().WithResult("Done", "session-1")
	require.NoError(t, (&SendCmd{Task: taskName, Prompt: "Do it"}).WithHarness(sendMock).Run())

	reviewMock := harness.NewMockHarness().WithReviewResult("Looks good")

	_, _, err := captureStdoutStderr(t, (&ReviewCmd{
		Task: taskName,
	}).WithHarness(reviewMock).Run)

	require.NoError(t, err)

	require.Len(t, reviewMock.ReviewCalls, 1)
	call := reviewMock.ReviewCalls[0]
	assert.Equal(t, "main", call.Target.BaseBranch)
	assert.Empty(t, call.Instructions)
}

func TestReviewCmd_Uncommitted(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	reviewMock := harness.NewMockHarness().WithReviewResult("No issues")

	stdout, stderr, err := captureStdoutStderr(t, (&ReviewCmd{
		Uncommitted: true,
	}).WithHarness(reviewMock).Run)

	require.NoError(t, err)
	require.Empty(t, stderr)
	assert.Contains(t, stdout, "No issues")

	require.Len(t, reviewMock.ReviewCalls, 1)
	call := reviewMock.ReviewCalls[0]
	assert.True(t, call.Target.Uncommitted)
}

func TestReviewCmd_BaseBranch(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	reviewMock := harness.NewMockHarness().WithReviewResult("No issues")

	stdout, stderr, err := captureStdoutStderr(t, (&ReviewCmd{
		Base: " main ",
	}).WithHarness(reviewMock).Run)

	require.NoError(t, err)
	require.Empty(t, stderr)
	assert.Contains(t, stdout, "No issues")

	require.Len(t, reviewMock.ReviewCalls, 1)
	call := reviewMock.ReviewCalls[0]
	assert.Equal(t, "main", call.Target.BaseBranch)
}

func TestReviewCmd_Commit(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	reviewMock := harness.NewMockHarness().WithReviewResult("Commit looks good")

	stdout, stderr, err := captureStdoutStderr(t, (&ReviewCmd{
		Commit: "abc1234",
	}).WithHarness(reviewMock).Run)

	require.NoError(t, err)
	require.Empty(t, stderr)
	assert.Contains(t, stdout, "Commit looks good")

	require.Len(t, reviewMock.ReviewCalls, 1)
	call := reviewMock.ReviewCalls[0]
	assert.Equal(t, "abc1234", call.Target.Commit)
}

func TestReviewCmd_MutuallyExclusive(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	_, _, err := captureStdoutStderr(t, (&ReviewCmd{
		Base:        "main",
		Uncommitted: true,
	}).Run)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestReviewCmd_RequiresTarget(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	_, _, err := captureStdoutStderr(t, (&ReviewCmd{}).Run)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "specify one of")
}

func TestReviewCmd_TaskNotFound(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	_, _, err := captureStdoutStderr(t, (&ReviewCmd{
		Task: "nonexistent/task",
	}).Run)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load task")
}

func TestReviewCmd_NoWorkspace(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	taskName := "review/no-workspace"

	// Create a draft task without running it
	_, _, err := captureStdoutStderr(t, (&DraftCmd{
		Task:        taskName,
		Description: "Description",
		Base:        "main",
		Title:       "Draft review",
	}).Run)
	require.NoError(t, err)

	// Review should fail because there's no workspace
	_, _, err = captureStdoutStderr(t, (&ReviewCmd{
		Task: taskName,
	}).Run)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no workspace")
	assert.Contains(t, err.Error(), "subtask send")
}

// TestReviewCmd_Task_UsesTaskAdapterOverProjectDefault verifies that when --task is
// set, the task's stored adapter is resolved instead of the project default. This
// covers the regression where a pi-default project used pi for tasks drafted via a
// claude-based preset, ignoring the task's snapshot.
func TestReviewCmd_Task_UsesTaskAdapterOverProjectDefault(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	// Override project config to "pi". In the test env the pi binary doesn't
	// exist, so if resolution falls through to the project default the review
	// will fail. The task snapshot sets "builtin-mock", which always succeeds.
	cfgPath := task.ConfigPath()
	cfg := &workspace.Config{
		Adapter:       "pi",
		MaxWorkspaces: workspace.DefaultMaxWorkspaces,
	}
	cfgData, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfgPath, cfgData, 0o644))

	taskName := "review/task-adapter"
	tk := env.CreateTask(taskName, "Adapter resolution test", "main", "Description")
	tk.Adapter = "builtin-mock"
	require.NoError(t, tk.Save())

	// Seed a workspace so the review command can find one.
	env.CreateTaskState(taskName, &task.State{Workspace: env.Workspaces[0]})

	// Review without WithHarness: resolution should pick up task's "builtin-mock".
	stdout, _, err := captureStdoutStderr(t, (&ReviewCmd{
		Task: taskName,
	}).Run)

	require.NoError(t, err)
	assert.NotEmpty(t, stdout)
}

// TestReviewCmd_Preset_OverridesProjectDefault verifies that --preset on review
// selects the preset's adapter over the project default.
func TestReviewCmd_Preset_OverridesProjectDefault(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	// Override project config: pi as default, preset that uses builtin-mock.
	cfgPath := task.ConfigPath()
	cfg := &workspace.Config{
		Adapter:       "pi",
		MaxWorkspaces: workspace.DefaultMaxWorkspaces,
		Presets: map[string]workspace.Preset{
			"use-mock": {Adapter: "builtin-mock"},
		},
	}
	cfgData, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfgPath, cfgData, 0o644))

	stdout, _, err := captureStdoutStderr(t, (&ReviewCmd{
		Uncommitted: true,
		Preset:      "use-mock",
	}).Run)

	require.NoError(t, err)
	assert.NotEmpty(t, stdout)
}

// TestReviewCmd_Adapter_OverridesProjectDefault verifies that --adapter on review
// selects the named adapter over the project default.
func TestReviewCmd_Adapter_OverridesProjectDefault(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	// Override project config to pi. Without the --adapter override, the pi
	// binary would be invoked (not present in test env) and fail.
	cfgPath := task.ConfigPath()
	cfg := &workspace.Config{
		Adapter:       "pi",
		MaxWorkspaces: workspace.DefaultMaxWorkspaces,
	}
	cfgData, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(cfgPath, cfgData, 0o644))

	stdout, _, err := captureStdoutStderr(t, (&ReviewCmd{
		Uncommitted: true,
		Adapter:     "builtin-mock",
	}).Run)

	require.NoError(t, err)
	assert.NotEmpty(t, stdout)
}

