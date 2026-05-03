package workspace_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workspace"
)

func TestLoadConfig_ValidatesTypePresetReference(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Types: map[string]workspace.TaskType{
			"impl": {DefaultPreset: "missing"},
		},
	}
	require.NoError(t, cfg.SaveTo(task.ConfigPath()))

	_, err := workspace.LoadConfig()
	require.Error(t, err)
	require.Contains(t, err.Error(), `type "impl" references unknown default_preset "missing"`)
}

func TestLoadConfig_AcceptsValidConfig(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"sonnet-medium": {Adapter: "claude", Model: "sonnet", Reasoning: "medium"},
			"opus-high":     {Adapter: "claude", Model: "opus", Reasoning: "high"},
		},
		Types: map[string]workspace.TaskType{
			"implement": {DefaultWorkflow: "they-plan", DefaultPreset: "sonnet-medium"},
			"review":    {DefaultPreset: "opus-high"},
		},
	}
	require.NoError(t, cfg.SaveTo(task.ConfigPath()))

	loaded, err := workspace.LoadConfig()
	require.NoError(t, err)
	require.Len(t, loaded.Presets, 2)
	require.Len(t, loaded.Types, 2)
	require.Equal(t, "sonnet", loaded.Presets["sonnet-medium"].Model)
	require.Equal(t, "they-plan", loaded.Types["implement"].DefaultWorkflow)
}
