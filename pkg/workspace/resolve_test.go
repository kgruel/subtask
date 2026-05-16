package workspace_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/workspace"
)

var baseConfig = &workspace.Config{
	Adapter: "claude",
	Model:   "sonnet",
}

func TestResolve_NoOverrides_UsesProjectDefault(t *testing.T) {
	r, err := workspace.Resolve(baseConfig, nil, workspace.ResolveOverrides{})
	require.NoError(t, err)
	require.Equal(t, "claude", r.Adapter)
	require.Equal(t, "sonnet", r.Model)
	require.Equal(t, "", r.Reasoning)
}

func TestResolve_SnapshotFallback(t *testing.T) {
	snap := &task.Task{Adapter: "codex", Model: "o3", Reasoning: "low"}
	r, err := workspace.Resolve(baseConfig, snap, workspace.ResolveOverrides{})
	require.NoError(t, err)
	require.Equal(t, "codex", r.Adapter)
	require.Equal(t, "o3", r.Model)
	require.Equal(t, "low", r.Reasoning)
}

func TestResolve_FlagOverridesSnapshot(t *testing.T) {
	snap := &task.Task{Adapter: "codex", Model: "o3", Reasoning: "low"}
	r, err := workspace.Resolve(baseConfig, snap, workspace.ResolveOverrides{
		Adapter:   "gemini",
		Model:     "flash",
		Reasoning: "medium",
	})
	require.NoError(t, err)
	require.Equal(t, "gemini", r.Adapter)
	require.Equal(t, "flash", r.Model)
	require.Equal(t, "medium", r.Reasoning)
}

func TestResolve_AgentOverlayApplied(t *testing.T) {
	spec := &workspace.AgentSpec{Adapter: "claude", Model: "opus", Reasoning: "high"}
	r, err := workspace.Resolve(baseConfig, nil, workspace.ResolveOverrides{
		Agent: spec,
	})
	require.NoError(t, err)
	require.Equal(t, "claude", r.Adapter)
	require.Equal(t, "opus", r.Model)
	require.Equal(t, "high", r.Reasoning)
}

func TestResolve_FlagBeatsAgent(t *testing.T) {
	// Explicit flag wins over agent overlay.
	spec := &workspace.AgentSpec{Adapter: "claude", Model: "opus", Reasoning: "high"}
	r, err := workspace.Resolve(baseConfig, nil, workspace.ResolveOverrides{
		Model: "haiku",
		Agent: spec,
	})
	require.NoError(t, err)
	require.Equal(t, "haiku", r.Model)    // flag wins
	require.Equal(t, "high", r.Reasoning) // agent fills the rest
}

func TestResolve_AgentBeatsSnapshot(t *testing.T) {
	// Agent overlays are applied before snapshot fallback, so agent wins.
	snap := &task.Task{Adapter: "codex", Model: "o3", Reasoning: "low"}
	spec := &workspace.AgentSpec{Adapter: "claude", Model: "opus", Reasoning: "high"}
	r, err := workspace.Resolve(baseConfig, snap, workspace.ResolveOverrides{
		Agent: spec,
	})
	require.NoError(t, err)
	// Agent fills all four flags, so snapshot is never reached.
	require.Equal(t, "claude", r.Adapter)
	require.Equal(t, "opus", r.Model)
	require.Equal(t, "high", r.Reasoning)
}

func TestResolve_AgentOnlyFillsEmptyFlags(t *testing.T) {
	// When agent's field is set but the flag is empty, agent fills it.
	// When agent's field is empty, snapshot/project-default fills it.
	snap := &task.Task{Adapter: "codex", Model: "o3"}
	spec := &workspace.AgentSpec{Adapter: "codex", Model: "", Reasoning: "medium"}
	r, err := workspace.Resolve(baseConfig, snap, workspace.ResolveOverrides{
		Agent: spec,
	})
	require.NoError(t, err)
	require.Equal(t, "codex", r.Adapter)
	require.Equal(t, "o3", r.Model)      // agent's model is empty, falls through to snapshot
	require.Equal(t, "medium", r.Reasoning)
}

func TestResolve_InvalidReasoning_Error(t *testing.T) {
	_, err := workspace.Resolve(baseConfig, nil, workspace.ResolveOverrides{
		Reasoning: "turbo", // not in validReasoningLevels
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), `invalid reasoning "turbo"`)
}

func TestResolve_InvalidReasoningFromSnapshot_Error(t *testing.T) {
	snap := &task.Task{Reasoning: "turbo"}
	_, err := workspace.Resolve(baseConfig, snap, workspace.ResolveOverrides{})
	require.Error(t, err)
	require.Contains(t, err.Error(), `invalid reasoning "turbo"`)
}

func TestApplyAgentSpec_OnlyFillsEmpty(t *testing.T) {
	p := workspace.AgentSpec{Adapter: "codex", Provider: "openai", Model: "o4", Reasoning: "high"}
	adapter, provider, model, reasoning := "claude", "", "", "low"
	workspace.ApplyAgentSpec(p, &adapter, &provider, &model, &reasoning)
	require.Equal(t, "claude", adapter)  // already set — not overwritten
	require.Equal(t, "openai", provider) // was empty — filled
	require.Equal(t, "o4", model)        // was empty — filled
	require.Equal(t, "low", reasoning)   // already set — not overwritten
}
