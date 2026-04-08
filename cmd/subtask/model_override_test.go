package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/workspace"
)

func TestResolveModel_Precedence(t *testing.T) {
	cfg := &workspace.Config{Model: "config-model"}
	tsk := &task.Task{Model: "task-model"}

	require.Equal(t, "send-model", workspace.ResolveModel(cfg, tsk, "send-model"))
	require.Equal(t, "task-model", workspace.ResolveModel(cfg, tsk, ""))

	tsk.Model = ""
	require.Equal(t, "config-model", workspace.ResolveModel(cfg, tsk, ""))
}

func TestResolveReasoning_Precedence(t *testing.T) {
	cfg := &workspace.Config{Reasoning: "medium"}
	tsk := &task.Task{Reasoning: "high"}

	require.Equal(t, "xhigh", workspace.ResolveReasoning(cfg, tsk, "xhigh"))
	require.Equal(t, "high", workspace.ResolveReasoning(cfg, tsk, ""))

	tsk.Reasoning = ""
	require.Equal(t, "medium", workspace.ResolveReasoning(cfg, tsk, ""))
}

func TestValidateReasoningLevel(t *testing.T) {
	require.NoError(t, workspace.ValidateReasoningLevel(""))
	require.NoError(t, workspace.ValidateReasoningLevel("high"))
	require.Error(t, workspace.ValidateReasoningLevel("nope"))
}

func TestValidateReasoningFlag_ClaudeErrors(t *testing.T) {
	err := workspace.ValidateReasoningFlag("claude", "high")
	require.Error(t, err)
	require.Contains(t, err.Error(), "codex-only")
}

func TestConfigWithModelReasoning_DoesNotMutateOriginal(t *testing.T) {
	cfg := &workspace.Config{
		Adapter: "codex",
		Model:   "old",
	}

	out := workspace.ConfigWithModelReasoning(cfg, "new", "high")
	require.Equal(t, "old", cfg.Model)

	require.Equal(t, "new", out.Model)
	require.Equal(t, "high", out.Reasoning)
}
