package main

import (
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

func TestDraft_TypeResolvesWorkflowAndPreset(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"sonnet-medium": {Adapter: "claude", Model: "sonnet", Reasoning: "medium"},
		},
		Types: map[string]workspace.TaskType{
			"implement": {DefaultWorkflow: "they-plan", DefaultPreset: "sonnet-medium"},
		},
	}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))

	draft := &DraftCmd{
		Task:        "type-task",
		Title:       "Type Test",
		Description: "Test that --type resolves",
		Base:        "main",
		Type:        "implement",
	}
	require.NoError(t, draft.Run())

	tObj, err := task.Load("type-task")
	require.NoError(t, err)
	require.Equal(t, "implement", tObj.Type)
	require.Equal(t, "claude", tObj.Adapter)
	require.Equal(t, "sonnet", tObj.Model)
	require.Equal(t, "medium", tObj.Reasoning)
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

func TestDraft_FollowUpInheritsType(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"sonnet-medium": {Adapter: "claude", Model: "sonnet", Reasoning: "medium"},
		},
		Types: map[string]workspace.TaskType{
			"implement": {DefaultWorkflow: "they-plan", DefaultPreset: "sonnet-medium"},
		},
	}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))

	parent := &DraftCmd{
		Task:        "parent-task",
		Title:       "Parent",
		Description: "Parent task",
		Base:        "main",
		Type:        "implement",
	}
	require.NoError(t, parent.Run())

	child := &DraftCmd{
		Task:        "child-task",
		Title:       "Child",
		Description: "Child task",
		Base:        "main",
		FollowUp:    "parent-task",
	}
	require.NoError(t, child.Run())

	tObj, err := task.Load("child-task")
	require.NoError(t, err)
	require.Equal(t, "implement", tObj.Type, "follow-up should inherit parent's type")
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
