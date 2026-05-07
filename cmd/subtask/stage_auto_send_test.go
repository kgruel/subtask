package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workspace"
)

const autoSendWorkflow = `name: auto-send
description: Workflow with worker_instructions on review stage
stages:
  - name: implement
    instructions: Draft the implementation plan.
  - name: review
    worker_instructions: Review the diff carefully and list any issues.
    instructions: Review stage (lead guidance).
  - name: ready
    instructions: Done.
`

const noInstructionsWorkflow = `name: no-instructions
description: Workflow with no worker_instructions on any stage
stages:
  - name: implement
    instructions: Do the work.
  - name: ready
    instructions: Done.
`

func setupAutoSendEnv(t *testing.T) (*testutil.TestEnv, *workspace.Config) {
	t.Helper()
	env := testutil.NewTestEnv(t, 1)
	cfg := &workspace.Config{Adapter: "claude"}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))
	return env, cfg
}

func TestStage_AutoDispatchesWhenWorkerInstructionsSet(t *testing.T) {
	env, _ := setupAutoSendEnv(t)
	withOutputMode(t, false)

	installCustomWorkflow(t, env, "auto-send", autoSendWorkflow)

	require.NoError(t, (&DraftCmd{
		Task:        "auto/dispatch",
		Title:       "Auto dispatch test",
		Description: "Tests stage auto-dispatch",
		Base:        "main",
		Workflow:    "auto-send",
	}).Run())

	mock := harness.NewMockHarness()
	err := (&StageCmd{Task: "auto/dispatch", Stage: "review"}).WithHarness(mock).Run()
	require.NoError(t, err)

	require.Equal(t, 1, mock.RunCallCount(), "worker should have been dispatched")
	prompt := mock.LastRunCall().Prompt
	require.Contains(t, prompt, "Review the diff carefully and list any issues.")
	// Regression: worker_instructions must appear exactly once. BuildPrompt
	// injects them into the "## Stage:" block; stage.go must not re-include
	// them in the user message.
	require.Equal(t, 1, strings.Count(prompt, "Review the diff carefully and list any issues."),
		"worker_instructions must not be duplicated")
}

func TestStage_PassiveWhenNoWorkerInstructions(t *testing.T) {
	env, _ := setupAutoSendEnv(t)
	withOutputMode(t, false)

	installCustomWorkflow(t, env, "no-instructions", noInstructionsWorkflow)

	require.NoError(t, (&DraftCmd{
		Task:        "passive/task",
		Title:       "Passive test",
		Description: "Tests stage passivity without worker_instructions",
		Base:        "main",
		Workflow:    "no-instructions",
	}).Run())

	mock := harness.NewMockHarness()
	err := (&StageCmd{Task: "passive/task", Stage: "ready"}).WithHarness(mock).Run()
	require.NoError(t, err)

	require.Equal(t, 0, mock.RunCallCount(), "worker should NOT be dispatched without worker_instructions")
}

func TestStage_NoSendSuppressesAutoDispatch(t *testing.T) {
	env, _ := setupAutoSendEnv(t)
	withOutputMode(t, false)

	installCustomWorkflow(t, env, "auto-send", autoSendWorkflow)

	require.NoError(t, (&DraftCmd{
		Task:        "nosend/task",
		Title:       "NoSend test",
		Description: "Tests --no-send flag",
		Base:        "main",
		Workflow:    "auto-send",
	}).Run())

	mock := harness.NewMockHarness()
	err := (&StageCmd{Task: "nosend/task", Stage: "review", NoSend: true}).WithHarness(mock).Run()
	require.NoError(t, err)

	require.Equal(t, 0, mock.RunCallCount(), "--no-send should suppress dispatch even with worker_instructions")
}

func TestStage_CombinesWorkerInstructionsWithPositionalPrompt(t *testing.T) {
	env, _ := setupAutoSendEnv(t)
	withOutputMode(t, false)

	installCustomWorkflow(t, env, "auto-send", autoSendWorkflow)

	require.NoError(t, (&DraftCmd{
		Task:        "combined/task",
		Title:       "Combined prompt test",
		Description: "Tests worker_instructions + extra brief",
		Base:        "main",
		Workflow:    "auto-send",
	}).Run())

	mock := harness.NewMockHarness()
	err := (&StageCmd{Task: "combined/task", Stage: "review", Prompt: "Focus on error handling."}).WithHarness(mock).Run()
	require.NoError(t, err)

	require.Equal(t, 1, mock.RunCallCount())
	sentPrompt := mock.LastRunCall().Prompt
	require.Contains(t, sentPrompt, "Review the diff carefully and list any issues.")
	require.Contains(t, sentPrompt, "Focus on error handling.")
	// Extra brief must appear after the worker_instructions separator.
	wiIdx := strings.Index(sentPrompt, "Review the diff carefully")
	extraIdx := strings.Index(sentPrompt, "Focus on error handling.")
	require.Greater(t, extraIdx, wiIdx, "extra brief should follow worker_instructions")
	// Regression: worker_instructions still must appear exactly once.
	require.Equal(t, 1, strings.Count(sentPrompt, "Review the diff carefully and list any issues."),
		"worker_instructions must not be duplicated when combined with positional prompt")
}

func TestStage_PositionalPromptWithoutWorkerInstructionsDispatches(t *testing.T) {
	env, _ := setupAutoSendEnv(t)
	withOutputMode(t, false)

	installCustomWorkflow(t, env, "no-instructions", noInstructionsWorkflow)

	require.NoError(t, (&DraftCmd{
		Task:        "prompt-only/task",
		Title:       "Prompt-only test",
		Description: "Tests positional prompt without worker_instructions",
		Base:        "main",
		Workflow:    "no-instructions",
	}).Run())

	mock := harness.NewMockHarness()
	err := (&StageCmd{Task: "prompt-only/task", Stage: "ready", Prompt: "Ship it."}).WithHarness(mock).Run()
	require.NoError(t, err)

	require.Equal(t, 1, mock.RunCallCount(), "positional prompt alone should trigger dispatch")
	require.Contains(t, mock.LastRunCall().Prompt, "Ship it.")
}

func TestStage_ErrorWhenWorkerAlreadyRunning(t *testing.T) {
	env, _ := setupAutoSendEnv(t)
	withOutputMode(t, false)

	installCustomWorkflow(t, env, "auto-send", autoSendWorkflow)

	require.NoError(t, (&DraftCmd{
		Task:        "running/task",
		Title:       "Running guard test",
		Description: "Tests guard against concurrent dispatch",
		Base:        "main",
		Workflow:    "auto-send",
	}).Run())

	// Simulate a worker in progress by setting a live SupervisorPID.
	require.NoError(t, (&task.State{SupervisorPID: os.Getpid()}).Save("running/task"))

	mock := harness.NewMockHarness()
	err := (&StageCmd{Task: "running/task", Stage: "review"}).WithHarness(mock).Run()
	require.Error(t, err)
	require.Contains(t, err.Error(), "working")

	require.Equal(t, 0, mock.RunCallCount(), "worker should not be dispatched when task is running")
}
