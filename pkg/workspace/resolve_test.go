package workspace_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/workspace"
)

var baseConfig = &workspace.Config{
	Adapter:   "claude",
	Model:     "sonnet",
	Reasoning: "",
	Presets: map[string]workspace.Preset{
		"opus-high": {Adapter: "claude", Model: "opus", Reasoning: "high"},
		"codex-med": {Adapter: "codex", Model: "", Reasoning: "medium"},
	},
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

func TestResolve_PresetOverlayApplied(t *testing.T) {
	r, err := workspace.Resolve(baseConfig, nil, workspace.ResolveOverrides{
		Preset: "opus-high",
	})
	require.NoError(t, err)
	require.Equal(t, "claude", r.Adapter)
	require.Equal(t, "opus", r.Model)
	require.Equal(t, "high", r.Reasoning)
}

func TestResolve_FlagBeatsPreset(t *testing.T) {
	// Explicit flag wins over preset.
	r, err := workspace.Resolve(baseConfig, nil, workspace.ResolveOverrides{
		Model:  "haiku",
		Preset: "opus-high",
	})
	require.NoError(t, err)
	require.Equal(t, "haiku", r.Model) // flag wins
	require.Equal(t, "high", r.Reasoning) // preset fills the rest
}

func TestResolve_PresetBeatsSnapshot(t *testing.T) {
	// Preset overrides are applied before snapshot lookup, so preset wins.
	snap := &task.Task{Adapter: "codex", Model: "o3", Reasoning: "low"}
	r, err := workspace.Resolve(baseConfig, snap, workspace.ResolveOverrides{
		Preset: "opus-high",
	})
	require.NoError(t, err)
	// Preset fills all four flags, so snapshot is never reached.
	require.Equal(t, "claude", r.Adapter)
	require.Equal(t, "opus", r.Model)
	require.Equal(t, "high", r.Reasoning)
}

func TestResolve_PresetOnlyFillsEmptyFlags(t *testing.T) {
	// When the preset's field is set but the flag is empty, preset fills it.
	// When the preset's field is empty, snapshot/project-default fills it.
	snap := &task.Task{Adapter: "codex", Model: "o3"}
	r, err := workspace.Resolve(baseConfig, snap, workspace.ResolveOverrides{
		Preset: "codex-med", // Adapter="codex", Model="", Reasoning="medium"
	})
	require.NoError(t, err)
	require.Equal(t, "codex", r.Adapter)
	require.Equal(t, "o3", r.Model)    // preset's model is empty, falls through to snapshot
	require.Equal(t, "medium", r.Reasoning)
}

func TestResolve_UnknownPreset_Error(t *testing.T) {
	_, err := workspace.Resolve(baseConfig, nil, workspace.ResolveOverrides{
		Preset: "nonexistent",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown preset "nonexistent"`)
	require.Contains(t, err.Error(), "Available:")
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

func TestResolve_PresetNames_EmptyMap(t *testing.T) {
	cfg := &workspace.Config{Adapter: "claude"}
	names := workspace.PresetNames(cfg)
	require.Equal(t, "(none defined)", names)
}

func TestResolve_PresetNames_Sorted(t *testing.T) {
	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"z-last":  {},
			"a-first": {},
			"m-mid":   {},
		},
	}
	names := workspace.PresetNames(cfg)
	parts := strings.Split(names, ", ")
	require.Equal(t, []string{"a-first", "m-mid", "z-last"}, parts)
}

func TestApplyPreset_OnlyFillsEmpty(t *testing.T) {
	p := workspace.Preset{Adapter: "codex", Provider: "openai", Model: "o4", Reasoning: "high"}
	adapter, provider, model, reasoning := "claude", "", "", "low"
	workspace.ApplyPreset(p, &adapter, &provider, &model, &reasoning)
	require.Equal(t, "claude", adapter)   // already set — not overwritten
	require.Equal(t, "openai", provider)  // was empty — filled from preset
	require.Equal(t, "o4", model)         // was empty — filled
	require.Equal(t, "low", reasoning)    // already set — not overwritten
}
