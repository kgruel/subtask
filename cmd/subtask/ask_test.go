package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/testutil"
)

// writeAgentFile creates a flat-schema agent YAML under .subtask/agents/<name>.yaml.
// Shared by ask_test.go and review_test.go (same package).
func writeAgentFile(t *testing.T, env *testutil.TestEnv, name, adapter, model, reasoning string) {
	t.Helper()
	dir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	content := fmt.Sprintf("adapter: %s\nmodel: %s\nreasoning: %s\n", adapter, model, reasoning)
	require.NoError(t, os.WriteFile(filepath.Join(dir, name+".yaml"), []byte(content), 0o644))
}

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

// TestAskCmd_AgentOverride verifies --agent injects adapter/model before resolution.
func TestAskCmd_AgentOverride(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	writeAgentFile(t, env, "fast-coder", "codex", "o3", "high")

	mock := harness.NewMockHarness().WithResult("answer", "sess-1")
	_, _, err := captureStdoutStderr(t, (&AskCmd{
		Prompt: "What is X?",
		Agent:  "fast-coder",
	}).WithHarness(mock).Run)

	// Agent resolves to codex — no mismatch with a fresh session (no state), so should succeed.
	require.NoError(t, err)
}

// TestAskCmd_UnknownAgentErrors verifies a clear error for an unrecognized agent.
func TestAskCmd_UnknownAgentErrors(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	_, _, err := captureStdoutStderr(t, (&AskCmd{
		Prompt: "What is X?",
		Agent:  "ghost-agent",
	}).Run)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost-agent")
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
