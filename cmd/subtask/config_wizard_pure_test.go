package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/zippoxer/subtask/pkg/workspace"
)

func TestResolveConfigValues_Defaults(t *testing.T) {
	values := resolveConfigValues(nil, configFlags{})
	require.Equal(t, "codex", values.Adapter)
	require.Equal(t, "gpt-5.2", values.Model)
	require.Equal(t, "high", values.Reasoning)
	require.Equal(t, workspace.DefaultMaxWorkspaces, values.MaxWorkspaces)
}

func TestResolveConfigValues_ExistingClaude_DefaultsModel_DropsReasoning(t *testing.T) {
	existing := &workspace.Config{
		Adapter:   "claude",
		Reasoning: "high",
		MaxWorkspaces: 7,
	}
	values := resolveConfigValues(existing, configFlags{})
	require.Equal(t, "claude", values.Adapter)
	require.Equal(t, "opus", values.Model)
	require.Empty(t, values.Reasoning)
	require.Equal(t, 7, values.MaxWorkspaces)
}

func TestResolveConfigValues_FlagsHarnessOverride_ResetsDependentDefaults(t *testing.T) {
	existing := &workspace.Config{
		Adapter:   "codex",
		Model:     "gpt-5.2-codex",
		Reasoning: "xhigh",
	}
	values := resolveConfigValues(existing, configFlags{Adapter: "claude"})
	require.Equal(t, "claude", values.Adapter)
	require.Equal(t, "opus", values.Model)
	require.Empty(t, values.Reasoning)
}

func TestResolveConfigValues_FlagsOverrideModelAndReasoning(t *testing.T) {
	values := resolveConfigValues(nil, configFlags{
		Adapter:   "codex",
		Model:     "gpt-5.2-codex",
		Reasoning: "medium",
	})
	require.Equal(t, "codex", values.Adapter)
	require.Equal(t, "gpt-5.2-codex", values.Model)
	require.Equal(t, "medium", values.Reasoning)
}

func TestValidateConfigValues_InvalidAdapter(t *testing.T) {
	err := validateConfigValues(configValues{Adapter: "nope"})
	require.ErrorContains(t, err, "unknown adapter")
}

func TestValidateConfigValues_ReasoningCodexOnly(t *testing.T) {
	err := validateConfigValues(configValues{Adapter: "claude", Reasoning: "high"})
	require.ErrorContains(t, err, "codex-only")
}

func TestValidateConfigValues_MaxWorkspacesNegative(t *testing.T) {
	err := validateConfigValues(configValues{Adapter: "codex", MaxWorkspaces: -1})
	require.ErrorContains(t, err, "max workspaces must be >= 0")
}

func TestBuildConfig_UsesDefaultsAndOmitsEmptyFields(t *testing.T) {
	cfg := buildConfig(configValues{Adapter: "codex", MaxWorkspaces: 0})
	require.Equal(t, "codex", cfg.Adapter)
	require.Equal(t, workspace.DefaultMaxWorkspaces, cfg.MaxWorkspaces)
	require.Empty(t, cfg.Model)
	require.Empty(t, cfg.Reasoning)
}

func TestBuildConfig_SetsModelAndReasoning(t *testing.T) {
	cfg := buildConfig(configValues{
		Adapter:   "codex",
		Model:     "gpt-5.2-codex",
		Reasoning: "high",
	})
	require.Equal(t, "codex", cfg.Adapter)
	require.Equal(t, "gpt-5.2-codex", cfg.Model)
	require.Equal(t, "high", cfg.Reasoning)
}
