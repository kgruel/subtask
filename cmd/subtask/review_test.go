package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
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

func TestReviewCmd_Task_DiffEventsAndPersistence(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "review/diff-events"
	env.CreateTask(taskName, "Diff events test", "main", "Description")
	env.CreateTaskState(taskName, &task.State{Workspace: env.Workspaces[0]})

	reviewMock := harness.NewMockHarness().WithReviewResult("Looks clean")

	_, _, err := captureStdoutStderr(t, (&ReviewCmd{
		Task:   taskName,
		Prompt: "Focus on correctness",
	}).WithHarness(reviewMock).Run)

	require.NoError(t, err)

	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, err)

	var started, finished *history.Event
	for i := range evs {
		switch evs[i].Type {
		case "review.started":
			started = &evs[i]
		case "review.finished":
			finished = &evs[i]
		}
	}
	require.NotNil(t, started, "review.started event missing")
	require.NotNil(t, finished, "review.finished event missing")

	var sd struct {
		RunID        string `json:"run_id"`
		Kind         string `json:"kind"`
		Adapter      string `json:"adapter"`
		Model        string `json:"model"`
		Instructions string `json:"instructions"`
	}
	require.NoError(t, json.Unmarshal(started.Data, &sd))
	assert.NotEmpty(t, sd.RunID)
	assert.Equal(t, "diff", sd.Kind)
	assert.Equal(t, "builtin-mock", sd.Adapter)
	assert.Equal(t, "gpt-5.2", sd.Model)
	assert.Equal(t, "Focus on correctness", sd.Instructions)

	var fd struct {
		RunID   string `json:"run_id"`
		Kind    string `json:"kind"`
		Outcome string `json:"outcome"`
		File    string `json:"file"`
	}
	require.NoError(t, json.Unmarshal(finished.Data, &fd))
	assert.Equal(t, sd.RunID, fd.RunID)
	assert.Equal(t, "diff", fd.Kind)
	assert.Equal(t, "success", fd.Outcome)
	assert.Contains(t, fd.File, "-diff-builtin-mock.md")

	// Review file should exist
	reviewsDir := task.ReviewsDir(taskName)
	entries, err := os.ReadDir(reviewsDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Contains(t, entries[0].Name(), "-diff-builtin-mock.md")
}

func TestReviewCmd_Task_DiffErrorPath(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "review/diff-error"
	env.CreateTask(taskName, "Diff error test", "main", "Description")
	env.CreateTaskState(taskName, &task.State{Workspace: env.Workspaces[0]})

	reviewMock := harness.NewMockHarness().WithReviewError(errors.New("review harness failed"))

	_, _, err := captureStdoutStderr(t, (&ReviewCmd{
		Task: taskName,
	}).WithHarness(reviewMock).Run)

	require.Error(t, err)

	evs, readErr := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, readErr)

	var finished *history.Event
	for i := range evs {
		if evs[i].Type == "review.finished" {
			finished = &evs[i]
		}
	}
	require.NotNil(t, finished, "review.finished event missing")

	var fd struct {
		Outcome string `json:"outcome"`
		Error   string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(finished.Data, &fd))
	assert.Equal(t, "error", fd.Outcome)
	assert.Contains(t, fd.Error, "review harness failed")

	// No review file should be created
	_, statErr := os.Stat(task.ReviewsDir(taskName))
	assert.True(t, os.IsNotExist(statErr), "reviews dir should not exist on error path")
}

func TestReviewCmd_Plan_AppendsHistoryEvents(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "review/plan-events"
	env.CreateTask(taskName, "Plan events test", "main", "Spec: implement X, handle Y")

	// Write PLAN.md so --plan can find it
	planPath := filepath.Join(task.Dir(taskName), "PLAN.md")
	require.NoError(t, os.WriteFile(planPath, []byte("## Plan\n\nStep 1: implement X"), 0o644))

	planMock := harness.NewMockHarness().WithResult("Plan looks good", "session-1")

	_, _, err := captureStdoutStderr(t, (&ReviewCmd{
		Task: taskName,
		Plan: true,
	}).WithHarness(planMock).Run)

	require.NoError(t, err)

	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, err)

	var started, finished *history.Event
	for i := range evs {
		switch evs[i].Type {
		case "review.started":
			started = &evs[i]
		case "review.finished":
			finished = &evs[i]
		}
	}
	require.NotNil(t, started, "review.started event missing")
	require.NotNil(t, finished, "review.finished event missing")

	var sd struct {
		Kind string `json:"kind"`
	}
	require.NoError(t, json.Unmarshal(started.Data, &sd))
	assert.Equal(t, "plan", sd.Kind)

	var fd struct {
		Kind    string `json:"kind"`
		Outcome string `json:"outcome"`
		File    string `json:"file"`
	}
	require.NoError(t, json.Unmarshal(finished.Data, &fd))
	assert.Equal(t, "plan", fd.Kind)
	assert.Equal(t, "success", fd.Outcome)
	assert.Contains(t, fd.File, "-plan-builtin-mock.md")
}

// TestReviewCmd_Task_PersistFailure verifies that a persistence failure causes
// review.finished to record outcome:"error" (not "success"), and the command
// returns an error. The review text is still printed to stdout.
func TestReviewCmd_Task_PersistFailure(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "review/persist-fail"
	env.CreateTask(taskName, "Persist failure test", "main", "Description")
	env.CreateTaskState(taskName, &task.State{Workspace: env.Workspaces[0]})

	// Place a regular file at the reviews dir path so MkdirAll fails.
	reviewsPath := task.ReviewsDir(taskName)
	require.NoError(t, os.MkdirAll(filepath.Dir(reviewsPath), 0o755))
	require.NoError(t, os.WriteFile(reviewsPath, []byte("block"), 0o644))

	reviewMock := harness.NewMockHarness().WithReviewResult("Review text")

	stdout, _, err := captureStdoutStderr(t, (&ReviewCmd{
		Task: taskName,
	}).WithHarness(reviewMock).Run)

	require.Error(t, err, "expected error when persist fails")
	assert.Contains(t, stdout, "Review text", "review text should still be printed to stdout")

	evs, readErr := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, readErr)

	var finished *history.Event
	for i := range evs {
		if evs[i].Type == "review.finished" {
			finished = &evs[i]
		}
	}
	require.NotNil(t, finished, "review.finished event missing")

	var fd struct {
		Outcome string `json:"outcome"`
		Error   string `json:"error"`
		File    string `json:"file"`
	}
	require.NoError(t, json.Unmarshal(finished.Data, &fd))
	assert.Equal(t, "error", fd.Outcome, "persist failure should record outcome:error")
	assert.NotEmpty(t, fd.Error, "error field should be set")
	assert.Empty(t, fd.File, "file should be empty when persist failed")
}

// TestReviewCmd_Task_EmitsArtifactProduced verifies that a successful task diff
// review emits both review.finished and artifact.produced events, and that the
// artifact.produced payload has the expected shape.
func TestReviewCmd_Task_EmitsArtifactProduced(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "review/artifact-event"
	env.CreateTask(taskName, "Artifact event test", "main", "Description")
	env.CreateTaskState(taskName, &task.State{Workspace: env.Workspaces[0]})

	reviewMock := harness.NewMockHarness().WithReviewResult("All good")

	_, _, err := captureStdoutStderr(t, (&ReviewCmd{Task: taskName}).WithHarness(reviewMock).Run)
	require.NoError(t, err)

	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, err)

	var finished, artifact *history.Event
	for i := range evs {
		switch evs[i].Type {
		case "review.finished":
			finished = &evs[i]
		case "artifact.produced":
			artifact = &evs[i]
		}
	}
	require.NotNil(t, finished, "review.finished event missing")
	require.NotNil(t, artifact, "artifact.produced event missing")

	var ad struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Kind string `json:"kind"`
	}
	require.NoError(t, json.Unmarshal(artifact.Data, &ad))
	assert.Equal(t, "review", ad.Kind)
	assert.NotEmpty(t, ad.Name)
	assert.Contains(t, ad.Path, "reviews/")
	assert.Equal(t, filepath.Base(ad.Path), ad.Name)
}

// TestReviewCmd_Plan_EmitsArtifactProduced verifies that a successful --plan
// review also emits artifact.produced.
func TestReviewCmd_Plan_EmitsArtifactProduced(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "review/plan-artifact-event"
	env.CreateTask(taskName, "Plan artifact event test", "main", "Spec: implement X")

	planPath := filepath.Join(task.Dir(taskName), "PLAN.md")
	require.NoError(t, os.WriteFile(planPath, []byte("## Plan\n\nStep 1: implement X"), 0o644))

	planMock := harness.NewMockHarness().WithResult("Plan looks good", "session-1")

	_, _, err := captureStdoutStderr(t, (&ReviewCmd{
		Task: taskName,
		Plan: true,
	}).WithHarness(planMock).Run)
	require.NoError(t, err)

	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	require.NoError(t, err)

	var artifact *history.Event
	for i := range evs {
		if evs[i].Type == "artifact.produced" {
			artifact = &evs[i]
		}
	}
	require.NotNil(t, artifact, "artifact.produced event missing")

	var ad struct {
		Kind string `json:"kind"`
		Path string `json:"path"`
	}
	require.NoError(t, json.Unmarshal(artifact.Data, &ad))
	assert.Equal(t, "review", ad.Kind)
	assert.Contains(t, ad.Path, "reviews/")
}

// TestReviewCmd_Task_FilenameUniqueness verifies that two back-to-back reviews
// produce two distinct files (run_id in filename prevents overwrites).
func TestReviewCmd_Task_FilenameUniqueness(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)

	taskName := "review/uniqueness"
	env.CreateTask(taskName, "Uniqueness test", "main", "Description")
	env.CreateTaskState(taskName, &task.State{Workspace: env.Workspaces[0]})

	mock := harness.NewMockHarness().WithReviewResult("Review text")

	_, _, err := captureStdoutStderr(t, (&ReviewCmd{Task: taskName}).WithHarness(mock).Run)
	require.NoError(t, err)

	_, _, err = captureStdoutStderr(t, (&ReviewCmd{Task: taskName}).WithHarness(mock).Run)
	require.NoError(t, err)

	entries, err := os.ReadDir(task.ReviewsDir(taskName))
	require.NoError(t, err)
	assert.Len(t, entries, 2, "two reviews should produce two distinct files")
}

// TestReviewSummary_EmptyDir verifies that loadReviewSummary returns Count==0
// when the reviews directory exists but contains no .md files.
func TestReviewSummary_EmptyDir(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	taskName := "review/empty-dir"
	_, _, err := captureStdoutStderr(t, (&DraftCmd{
		Task:        taskName,
		Description: "Test",
		Base:        "main",
		Title:       "Empty dir test",
	}).Run)
	require.NoError(t, err)

	// Create the reviews dir but put no .md files in it.
	require.NoError(t, os.MkdirAll(task.ReviewsDir(taskName), 0o755))

	rs := loadReviewSummary(taskName)
	assert.Equal(t, 0, rs.Count, "empty reviews dir should report Count==0")
}

