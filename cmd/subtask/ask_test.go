package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workspace"
)

// TestAskCmd_FollowUpTask_UsesTaskAdapter verifies that when --follow-up names a task
// whose adapter matches its state, no harness-mismatch error fires — even when the
// project default is a different adapter.
func TestAskCmd_FollowUpTask_UsesTaskAdapter(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "ask/follow-up-adapter"
	env.CreateTask(taskName, "Test task", "main", "desc")

	// Task was run with codex; state records the session adapter.
	loaded, err := task.Load(taskName)
	require.NoError(t, err)
	loaded.Adapter = "codex"
	require.NoError(t, loaded.Save())

	env.CreateTaskState(taskName, &task.State{
		SessionID: "sess-codex",
		Adapter:   "codex",
	})

	// Project default is claude — without the fix, resolved adapter would be claude
	// and enforceTaskHarnessMatch would fire (codex != claude).
	setProjectAdapter(t, "claude", "claude-sonnet-4-5")

	mock := harness.NewMockHarness().WithResult("answer", "sess-codex-2")
	_, _, err = captureStdoutStderr(t, (&AskCmd{
		Prompt:   "What is X?",
		FollowUp: taskName,
	}).WithHarness(mock).Run)

	// Should succeed: task.Adapter=codex resolves to codex, matches state.Adapter=codex.
	require.NoError(t, err)
}

// TestAskCmd_FollowUpTask_HarnessMismatchErrors verifies that when the task has no
// adapter override (so project default resolves) and the state adapter differs, the
// mismatch error fires against the resolved adapter.
func TestAskCmd_FollowUpTask_HarnessMismatchErrors(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "ask/follow-up-mismatch"
	env.CreateTask(taskName, "Test task", "main", "desc")

	// Task has no adapter override — resolution falls through to project default (claude).
	// State was saved when the task ran under codex.
	env.CreateTaskState(taskName, &task.State{
		SessionID: "sess-codex",
		Adapter:   "codex",
	})

	setProjectAdapter(t, "claude", "claude-sonnet-4-5")

	_, _, err := captureStdoutStderr(t, (&AskCmd{
		Prompt:   "What is X?",
		FollowUp: taskName,
	}).Run)

	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "sessions are not compatible")
}

// TestAskCmd_AdapterFlag overrides the adapter explicitly via --adapter.
func TestAskCmd_AdapterFlag(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	taskName := "ask/adapter-flag"
	env.CreateTask(taskName, "Test task", "main", "desc")

	// State adapter is codex; --adapter codex should resolve to codex so no mismatch.
	env.CreateTaskState(taskName, &task.State{
		SessionID: "sess-codex",
		Adapter:   "codex",
	})

	setProjectAdapter(t, "claude", "claude-sonnet-4-5")

	mock := harness.NewMockHarness().WithResult("answer", "sess-codex-2")
	_, _, err := captureStdoutStderr(t, (&AskCmd{
		Prompt:   "What is X?",
		FollowUp: taskName,
		Adapter:  "codex",
	}).WithHarness(mock).Run)

	require.NoError(t, err)
}

// TestAskCmd_PresetOverride verifies --preset injects adapter/model before resolution.
func TestAskCmd_PresetOverride(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	cfg, err := workspace.LoadConfig()
	require.NoError(t, err)
	cfg.Adapter = "claude"
	cfg.Presets = map[string]workspace.Preset{
		"codex-high": {Adapter: "codex", Model: "o3", Reasoning: "high"},
	}
	require.NoError(t, cfg.Save())

	mock := harness.NewMockHarness().WithResult("answer", "sess-1")
	_, _, err = captureStdoutStderr(t, (&AskCmd{
		Prompt: "What is X?",
		Preset: "codex-high",
	}).WithHarness(mock).Run)

	// Preset resolves to codex — no mismatch with a fresh session (no state), so should succeed.
	require.NoError(t, err)
}

// TestAskCmd_UnknownPresetErrors verifies a clear error for an unrecognized preset.
func TestAskCmd_UnknownPresetErrors(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	_, _, err := captureStdoutStderr(t, (&AskCmd{
		Prompt: "What is X?",
		Preset: "nonexistent",
	}).Run)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown preset")
}

// TestAskCmd_InvalidReasoningErrors verifies that a bad --reasoning value is caught
// against the resolved adapter, even when a testHarness is injected.
func TestAskCmd_InvalidReasoningErrors(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	setProjectAdapter(t, "claude", "claude-sonnet-4-5")

	mock := harness.NewMockHarness()
	_, _, err := captureStdoutStderr(t, (&AskCmd{
		Prompt:    "What is X?",
		Reasoning: "turbo",
	}).WithHarness(mock).Run)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid reasoning")
}
