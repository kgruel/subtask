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

func TestConfigWithOverrides_OverlayAndNoMutation(t *testing.T) {
	cfg := &workspace.Config{
		Adapter:   "codex",
		Provider:  "openai",
		Model:     "old",
		Reasoning: "high",
	}

	// Non-empty overrides apply; empty overrides PRESERVE the existing value
	// (set-or-preserve), and the original is never mutated.
	out := workspace.ConfigWithOverrides(cfg, "", "", "new", "")
	require.Equal(t, "old", cfg.Model, "original must not be mutated")

	require.Equal(t, "codex", out.Adapter, "empty adapter preserves")
	require.Equal(t, "openai", out.Provider, "empty provider preserves, does not clear")
	require.Equal(t, "new", out.Model, "non-empty model overrides")
	require.Equal(t, "high", out.Reasoning, "empty reasoning preserves, does not clear")

	// A full override replaces every field.
	full := workspace.ConfigWithOverrides(cfg, "claude", "anthropic", "opus", "medium")
	require.Equal(t, "claude", full.Adapter)
	require.Equal(t, "anthropic", full.Provider)
	require.Equal(t, "opus", full.Model)
	require.Equal(t, "medium", full.Reasoning)
}
