package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/testutil"
	"github.com/kgruel/subtask/pkg/workspace"
)

// emptyCfg returns a Config with no presets — used by tests that don't exercise preset validation.
func emptyCfg() *workspace.Config {
	return &workspace.Config{Presets: map[string]workspace.Preset{}}
}

func TestList_EmptyWhenNoAgentsDir(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	summaries, _, err := List(emptyCfg())
	require.NoError(t, err)
	require.Empty(t, summaries)
}

func TestList_EmptyWhenAgentsDirEmpty(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	summaries, _, err := List(emptyCfg())
	require.NoError(t, err)
	require.Empty(t, summaries)
}

func TestList_NamedPresetAgent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(`
preset: opus-high
prompt:
  text: You are the planner.
`), 0o644))

	summaries, _, err := List(emptyCfg())
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, "planner", summaries[0].Name)
	require.Equal(t, "opus-high", summaries[0].PresetLabel)
	require.Equal(t, "text", summaries[0].PromptSource)
}

func TestList_InlinePresetAgent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "impl.yaml"), []byte(`
preset:
  adapter: codex
  model: gpt-5.5
  reasoning: high
prompt:
  text: You are the implementer.
`), 0o644))

	summaries, _, err := List(emptyCfg())
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, "impl", summaries[0].Name)
	require.Equal(t, "inline: codex/gpt-5.5/high", summaries[0].PresetLabel)
	require.Equal(t, "text", summaries[0].PromptSource)
}

func TestList_FilePromptAgent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	promptsDir := filepath.Join(env.RootDir, ".subtask", "prompts")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.MkdirAll(promptsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(promptsDir, "role.md"), []byte("You are a role."), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "reviewer.yaml"), []byte(`
preset: sonnet-medium
prompt:
  file: prompts/role.md
`), 0o644))

	summaries, _, err := List(emptyCfg())
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, "reviewer", summaries[0].Name)
	require.Equal(t, "sonnet-medium", summaries[0].PresetLabel)
	require.Equal(t, "file:prompts/role.md", summaries[0].PromptSource)
}

func TestList_SortedAlphabetically(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	for _, name := range []string{"zebra", "alpha", "mango"} {
		require.NoError(t, os.WriteFile(filepath.Join(agentsDir, name+".yaml"), []byte(`
preset: opus-high
prompt:
  text: Agent `+name+`.
`), 0o644))
	}

	summaries, _, err := List(emptyCfg())
	require.NoError(t, err)
	require.Len(t, summaries, 3)
	require.Equal(t, "alpha", summaries[0].Name)
	require.Equal(t, "mango", summaries[1].Name)
	require.Equal(t, "zebra", summaries[2].Name)
}

// TestList_PresetValidation verifies that PresetValid is true when a named
// preset exists in config, false when it doesn't, and true for inline presets
// and agents with no preset.
func TestList_PresetValidation(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "valid-agent.yaml"), []byte(`
preset: known-preset
prompt:
  text: Uses a defined preset.
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "broken-agent.yaml"), []byte(`
preset: ghost-preset
prompt:
  text: References a preset that does not exist.
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "inline-agent.yaml"), []byte(`
preset:
  adapter: claude
  model: sonnet
prompt:
  text: Inline preset is always valid.
`), 0o644))

	cfg := &workspace.Config{
		Presets: map[string]workspace.Preset{
			"known-preset": {Adapter: "claude", Model: "sonnet"},
		},
	}
	summaries, _, err := List(cfg)
	require.NoError(t, err)
	require.Len(t, summaries, 3)

	byName := make(map[string]AgentSummary, len(summaries))
	for _, s := range summaries {
		byName[s.Name] = s
	}

	require.True(t, byName["valid-agent"].PresetValid, "defined preset must be valid")
	require.False(t, byName["broken-agent"].PresetValid, "undefined preset must be invalid")
	require.True(t, byName["inline-agent"].PresetValid, "inline preset is always valid")
}
