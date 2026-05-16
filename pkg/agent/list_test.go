package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/testutil"
)

func TestList_EmptyWhenNoAgentsDir(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	summaries, _, err := List()
	require.NoError(t, err)
	require.Empty(t, summaries)
}

func TestList_EmptyWhenAgentsDirEmpty(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	summaries, _, err := List()
	require.NoError(t, err)
	require.Empty(t, summaries)
}

func TestList_FlatDispatchAgent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(`
adapter: claude
model: opus
reasoning: high
prompt:
  text: You are the planner.
`), 0o644))

	summaries, _, err := List()
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, "planner", summaries[0].Name)
	require.Equal(t, "claude", summaries[0].Adapter)
	require.Equal(t, "opus", summaries[0].Model)
	require.Equal(t, "high", summaries[0].Reasoning)
	require.Equal(t, "text", summaries[0].PromptSource)
}

func TestList_BareDispatchAgent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "impl.yaml"), []byte(`
adapter: codex
model: gpt-5.5
reasoning: high
`), 0o644))

	summaries, _, err := List()
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, "impl", summaries[0].Name)
	require.Equal(t, "codex", summaries[0].Adapter)
	require.Equal(t, "gpt-5.5", summaries[0].Model)
	require.Empty(t, summaries[0].PromptSource)
}

func TestList_FilePromptAgent(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	promptsDir := filepath.Join(env.RootDir, ".subtask", "prompts")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.MkdirAll(promptsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(promptsDir, "role.md"), []byte("You are a role."), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "reviewer.yaml"), []byte(`
adapter: claude
model: sonnet
prompt:
  file: prompts/role.md
`), 0o644))

	summaries, _, err := List()
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	require.Equal(t, "reviewer", summaries[0].Name)
	require.Equal(t, "claude", summaries[0].Adapter)
	require.Equal(t, "sonnet", summaries[0].Model)
	require.Equal(t, "file:prompts/role.md", summaries[0].PromptSource)
}

func TestList_SortedAlphabetically(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	for _, name := range []string{"zebra", "alpha", "mango"} {
		require.NoError(t, os.WriteFile(filepath.Join(agentsDir, name+".yaml"), []byte(`
adapter: claude
model: sonnet
prompt:
  text: Agent `+name+`.
`), 0o644))
	}

	summaries, _, err := List()
	require.NoError(t, err)
	require.Len(t, summaries, 3)
	require.Equal(t, "alpha", summaries[0].Name)
	require.Equal(t, "mango", summaries[1].Name)
	require.Equal(t, "zebra", summaries[2].Name)
}
