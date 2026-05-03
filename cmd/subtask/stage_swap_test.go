package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workspace"
)

// installCustomWorkflow drops a project-local workflow YAML at
// .subtask/workflows/<name>/WORKFLOW.yaml so the test can exercise per-stage
// preset bindings without depending on built-in templates being mutated.
func installCustomWorkflow(t *testing.T, env *testutil.TestEnv, name, body string) {
	t.Helper()
	dir := filepath.Join(env.RootDir, ".subtask", "workflows", name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "WORKFLOW.yaml"), []byte(body), 0o644))
}

func TestStage_SwapsPresetWhenStageHasBinding(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"sonnet-medium": {Adapter: "claude", Model: "sonnet", Reasoning: "medium"},
			"opus-high":     {Adapter: "claude", Model: "opus", Reasoning: "high"},
		},
	}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))

	installCustomWorkflow(t, env, "swap-flow", `name: swap-flow
description: Test workflow with per-stage presets
stages:
  - name: plan
    preset: sonnet-medium
    instructions: Plan the work.
  - name: review
    preset: opus-high
    instructions: Review with the strong model.
  - name: ready
    instructions: Done.
`)

	// Draft uses workflow first-stage's preset as starting harness.
	require.NoError(t, (&DraftCmd{
		Task:        "swap-task",
		Title:       "Swap test",
		Description: "Test the per-stage swap",
		Base:        "main",
		Workflow:    "swap-flow",
	}).Run())

	tObj, err := task.Load("swap-task")
	require.NoError(t, err)
	require.Equal(t, "claude", tObj.Adapter, "draft picks first stage's preset")
	require.Equal(t, "sonnet", tObj.Model)

	// Mock a session so we can verify it survives a same-adapter swap.
	require.NoError(t, (&task.State{
		SessionID: "session-on-sonnet",
		Adapter:   "claude",
	}).Save("swap-task"))

	// Stage to review — model swaps to opus, adapter unchanged, session preserved.
	require.NoError(t, (&StageCmd{Task: "swap-task", Stage: "review"}).Run())

	tObj, _ = task.Load("swap-task")
	require.Equal(t, "claude", tObj.Adapter)
	require.Equal(t, "opus", tObj.Model)
	require.Equal(t, "high", tObj.Reasoning)

	st, _ := task.LoadState("swap-task")
	require.Equal(t, "session-on-sonnet", st.SessionID, "same-adapter swap preserves session")

	// History records from_preset/to_preset.
	evs, err := history.Read("swap-task", history.ReadOptions{EventsOnly: true})
	require.NoError(t, err)
	foundReviewSwap := false
	for _, ev := range evs {
		if ev.Type != "stage.changed" {
			continue
		}
		var d struct {
			From       string `json:"from"`
			To         string `json:"to"`
			FromPreset string `json:"from_preset"`
			ToPreset   string `json:"to_preset"`
		}
		_ = json.Unmarshal(ev.Data, &d)
		if d.To == "review" && d.ToPreset == "opus-high" {
			foundReviewSwap = true
			require.Equal(t, "sonnet-medium", d.FromPreset)
		}
	}
	require.True(t, foundReviewSwap, "stage.changed should record from/to presets")
}

func TestStage_ClearsSessionOnAdapterSwap(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"sonnet-medium": {Adapter: "claude", Model: "sonnet", Reasoning: "medium"},
			"gpt5-low":      {Adapter: "codex", Model: "gpt-5", Reasoning: "low"},
		},
	}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))

	installCustomWorkflow(t, env, "cross-adapter", `name: cross-adapter
description: Cross-adapter swap test
stages:
  - name: plan
    preset: sonnet-medium
    instructions: Plan.
  - name: implement
    preset: gpt5-low
    instructions: Implement on codex.
  - name: ready
    instructions: Done.
`)

	require.NoError(t, (&DraftCmd{
		Task:        "cross-task",
		Title:       "Cross-adapter test",
		Description: "Verify session clears",
		Base:        "main",
		Workflow:    "cross-adapter",
	}).Run())

	require.NoError(t, (&task.State{
		SessionID: "claude-session-id",
		Adapter:   "claude",
	}).Save("cross-task"))

	require.NoError(t, (&StageCmd{Task: "cross-task", Stage: "implement"}).Run())

	tObj, _ := task.Load("cross-task")
	require.Equal(t, "codex", tObj.Adapter, "swap to codex applied")
	require.Equal(t, "gpt-5", tObj.Model)

	st, _ := task.LoadState("cross-task")
	require.Equal(t, "", st.SessionID, "session cleared on adapter swap")
	require.Equal(t, "codex", st.Adapter)
}

func TestStage_StaysOnLastPresetWhenStageUnbound(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"sonnet-medium": {Adapter: "claude", Model: "sonnet", Reasoning: "medium"},
		},
	}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))

	installCustomWorkflow(t, env, "partial", `name: partial
description: Some stages unbound
stages:
  - name: plan
    preset: sonnet-medium
    instructions: Plan.
  - name: implement
    instructions: Implement (no preset binding).
  - name: ready
    instructions: Done.
`)

	require.NoError(t, (&DraftCmd{
		Task:        "partial-task",
		Title:       "Partial",
		Description: "Stages without bindings keep last preset",
		Base:        "main",
		Workflow:    "partial",
	}).Run())

	tObj, _ := task.Load("partial-task")
	require.Equal(t, "sonnet", tObj.Model)

	// Stage to implement — no preset binding → harness unchanged.
	require.NoError(t, (&StageCmd{Task: "partial-task", Stage: "implement"}).Run())

	tObj, _ = task.Load("partial-task")
	require.Equal(t, "claude", tObj.Adapter, "unbound stage stays on last preset")
	require.Equal(t, "sonnet", tObj.Model)
}

func TestDraft_RejectsWorkflowWithUnknownPresetReference(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	cfg := &workspace.Config{
		Adapter: "claude",
		Presets: map[string]workspace.Preset{
			"sonnet-medium": {Adapter: "claude", Model: "sonnet"},
		},
	}
	require.NoError(t, cfg.SaveTo(filepath.Join(env.RootDir, ".subtask", "config.json")))

	installCustomWorkflow(t, env, "broken", `name: broken
description: References a missing preset
stages:
  - name: plan
    preset: missing-preset
    instructions: Plan.
  - name: ready
    instructions: Done.
`)

	err := (&DraftCmd{
		Task:        "broken-task",
		Title:       "Broken",
		Description: "Workflow refs missing preset",
		Base:        "main",
		Workflow:    "broken",
	}).Run()
	require.Error(t, err)
	require.Contains(t, err.Error(), `unknown preset "missing-preset"`)
	require.Contains(t, err.Error(), "sonnet-medium") // available list
}
