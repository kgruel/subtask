package workspace_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workspace"
)

func TestLoadConfig_AcceptsValidConfig(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"sonnet-medium": {Adapter: "claude", Model: "sonnet", Reasoning: "medium"},
			"opus-high":     {Adapter: "claude", Model: "opus", Reasoning: "high"},
		},
	}
	require.NoError(t, cfg.SaveTo(task.ConfigPath()))

	loaded, err := workspace.LoadConfig()
	require.NoError(t, err)
	require.Len(t, loaded.Presets, 2)
	require.Equal(t, "sonnet", loaded.Presets["sonnet-medium"].Model)
}
