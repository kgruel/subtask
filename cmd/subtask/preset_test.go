package main

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workspace"
)

func TestDraft_PresetResolvesAdapterAndModel(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"opus-high": {Adapter: "claude", Model: "opus", Reasoning: "high"},
		},
	}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))

	draft := &DraftCmd{
		Task:        "preset-task",
		Title:       "Preset Test",
		Description: "Test that --preset resolves",
		Base:        "main",
		Preset:      "opus-high",
	}
	require.NoError(t, draft.Run())

	tObj, err := task.Load("preset-task")
	require.NoError(t, err)
	require.Equal(t, "claude", tObj.Adapter)
	require.Equal(t, "opus", tObj.Model)
	require.Equal(t, "high", tObj.Reasoning)
}

func TestDraft_ExplicitFlagsBeatPreset(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"opus-high": {Adapter: "claude", Model: "opus", Reasoning: "high"},
		},
	}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))

	draft := &DraftCmd{
		Task:        "override-task",
		Title:       "Override Test",
		Description: "Explicit flag wins over preset field",
		Base:        "main",
		Preset:      "opus-high",
		Model:       "sonnet", // explicit override
	}
	require.NoError(t, draft.Run())

	tObj, err := task.Load("override-task")
	require.NoError(t, err)
	require.Equal(t, "claude", tObj.Adapter) // from preset
	require.Equal(t, "sonnet", tObj.Model)   // explicit wins
	require.Equal(t, "high", tObj.Reasoning) // from preset
}

func TestDraft_UnknownPresetErrors(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"sonnet-medium": {Adapter: "claude", Model: "sonnet"},
		},
	}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))

	draft := &DraftCmd{
		Task:        "bad-preset",
		Title:       "Bad",
		Description: "Description",
		Base:        "main",
		Preset:      "nonexistent",
	}
	err := draft.Run()
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown preset "nonexistent"`)
	require.Contains(t, err.Error(), "sonnet-medium") // available list
}

func TestPresetsCmd_JSON_ParsesClean(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"opus-high":     {Adapter: "claude", Model: "opus", Reasoning: "high"},
			"sonnet-medium": {Adapter: "claude", Model: "sonnet"},
		},
	}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))

	stdout, stderr, err := captureStdoutStderr(t, (&PresetsCmd{JSON: true}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)

	var items []presetJSONItem
	require.NoError(t, json.Unmarshal([]byte(stdout), &items))
	require.Len(t, items, 2)

	byName := make(map[string]presetJSONItem)
	for _, it := range items {
		byName[it.Name] = it
	}

	opus := byName["opus-high"]
	require.Equal(t, "claude", opus.Adapter)
	require.Equal(t, "opus", opus.Model)
	require.Equal(t, "high", opus.Reasoning)
	require.Empty(t, opus.Provider)

	sonnet := byName["sonnet-medium"]
	require.Equal(t, "claude", sonnet.Adapter)
	require.Equal(t, "sonnet", sonnet.Model)
	require.Empty(t, sonnet.Reasoning)
}

func TestPresetsCmd_JSON_Empty(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{Adapter: "claude"}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))

	stdout, stderr, err := captureStdoutStderr(t, (&PresetsCmd{JSON: true}).Run)
	require.NoError(t, err)
	require.Empty(t, stderr)

	var items []presetJSONItem
	require.NoError(t, json.Unmarshal([]byte(stdout), &items))
	require.NotNil(t, items)
	require.Empty(t, items)
}
