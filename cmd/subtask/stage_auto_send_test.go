package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
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

const contextOnlyWorkflow = `name: context-only
description: Workflow whose stage has worker_context (passive) but no worker_instructions
stages:
  - name: implement
    worker_context: Commit your work when done.
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

func TestStage_WorkerContextDoesNotAutoDispatch(t *testing.T) {
	env, _ := setupAutoSendEnv(t)
	withOutputMode(t, false)

	installCustomWorkflow(t, env, "context-only", contextOnlyWorkflow)

	require.NoError(t, (&DraftCmd{
		Task:        "ctx/passive",
		Title:       "Context-only test",
		Description: "Passive worker_context should not trigger dispatch",
		Base:        "main",
		Workflow:    "context-only",
	}).Run())

	mock := harness.NewMockHarness()
	err := (&StageCmd{Task: "ctx/passive", Stage: "ready"}).WithHarness(mock).Run()
	require.NoError(t, err)

	require.Equal(t, 0, mock.RunCallCount(),
		"worker_context must NOT trigger auto-dispatch on its own — only worker_instructions does")
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

const autoAdvanceWorkflow = `name: auto-advance
description: "Workflow with advance: auto on the doing stage"
stages:
  - name: doing
    advance: auto
    instructions: Do the work.
  - name: review
    instructions: Review the work.
  - name: ready
    instructions: Done.
`

const autoAdvanceLastStageWorkflow = `name: auto-advance-last
description: "Workflow with advance: auto on the last stage"
stages:
  - name: doing
    instructions: Do the work.
  - name: ready
    advance: auto
    instructions: Done.
`

func TestSend_AutoAdvancesStageOnWorkerReply(t *testing.T) {
	env, _ := setupAutoSendEnv(t)
	withOutputMode(t, false)

	installCustomWorkflow(t, env, "auto-advance", autoAdvanceWorkflow)

	require.NoError(t, (&DraftCmd{
		Task:        "auto/advance",
		Title:       "Auto advance test",
		Description: "Tests auto-advance on worker reply",
		Base:        "main",
		Workflow:    "auto-advance",
	}).Run())

	mock := harness.NewMockHarness().WithResult("Done", "sess-1")
	_, _, err := captureStdoutStderr(t, (&SendCmd{Task: "auto/advance", Prompt: "Do it"}).WithHarness(mock).Run)
	require.NoError(t, err)

	// history.Tail must reflect the auto-advanced stage.
	tail, err := history.Tail("auto/advance")
	require.NoError(t, err)
	require.Equal(t, "review", tail.Stage, "tail.Stage should be 'review' after auto-advance")

	// A stage.changed event must be present with the correct from/to.
	events, err := history.Read("auto/advance", history.ReadOptions{})
	require.NoError(t, err)
	var stageChanged bool
	for _, ev := range events {
		if ev.Type != "stage.changed" {
			continue
		}
		var d map[string]any
		require.NoError(t, json.Unmarshal(ev.Data, &d))
		if d["from"] == "doing" && d["to"] == "review" {
			stageChanged = true
		}
	}
	require.True(t, stageChanged, "expected stage.changed event with from=doing to=review")
}

const autoAdvanceCrossAdapterWorkflow = `name: auto-advance-swap
description: "Workflow with advance: auto and a cross-adapter preset on the next stage"
stages:
  - name: doing
    advance: auto
    instructions: Do the work.
  - name: review
    preset: alt-adapter
    instructions: Review the work.
  - name: ready
    instructions: Done.
`

func TestSend_AutoAdvanceSwapsAdapterAndClearsSession(t *testing.T) {
	env := testutil.NewTestEnv(t, 1)
	withOutputMode(t, false)

	// Config with a preset that uses a different adapter so the auto-advance
	// swap is cross-adapter, triggering the session-clear path.
	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"alt-adapter": {Adapter: "codex"},
		},
	}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))

	installCustomWorkflow(t, env, "auto-advance-swap", autoAdvanceCrossAdapterWorkflow)

	require.NoError(t, (&DraftCmd{
		Task:        "swap/advance",
		Title:       "Cross-adapter auto-advance test",
		Description: "Tests that adapter swap and session clear happen after auto-advance",
		Base:        "main",
		Workflow:    "auto-advance-swap",
	}).Run())

	mock := harness.NewMockHarness().WithResult("Done", "sess-swap")
	_, _, err := captureStdoutStderr(t, (&SendCmd{Task: "swap/advance", Prompt: "Do it"}).WithHarness(mock).Run)
	require.NoError(t, err)

	// Stage advanced.
	tail, err := history.Tail("swap/advance")
	require.NoError(t, err)
	require.Equal(t, "review", tail.Stage, "stage should advance to 'review'")

	// Cross-adapter swap: session cleared and adapter updated.
	state, err := task.LoadState("swap/advance")
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Empty(t, state.SessionID, "session must be cleared on cross-adapter auto-advance")
	require.Equal(t, "codex", state.Adapter, "adapter must be updated to the next stage's preset adapter")
}

func TestSend_AutoAdvanceNoOpOnLastStage(t *testing.T) {
	env, _ := setupAutoSendEnv(t)
	withOutputMode(t, false)

	installCustomWorkflow(t, env, "auto-advance-last", autoAdvanceLastStageWorkflow)

	require.NoError(t, (&DraftCmd{
		Task:        "auto/last-stage",
		Title:       "Auto advance last stage test",
		Description: "Tests that advance: auto on last stage is a no-op",
		Base:        "main",
		Workflow:    "auto-advance-last",
	}).Run())

	// Advance to the last stage manually (passive — no worker_instructions to dispatch).
	require.NoError(t, (&StageCmd{Task: "auto/last-stage", Stage: "ready", NoSend: true}).Run())

	mock := harness.NewMockHarness().WithResult("Done", "sess-2")
	_, _, err := captureStdoutStderr(t, (&SendCmd{Task: "auto/last-stage", Prompt: "Finish it"}).WithHarness(mock).Run)
	require.NoError(t, err)

	tail, err := history.Tail("auto/last-stage")
	require.NoError(t, err)
	require.Equal(t, "ready", tail.Stage, "stage should remain 'ready' when advance: auto is on the last stage")
}
