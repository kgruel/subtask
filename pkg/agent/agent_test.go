package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/testutil"
)

func TestParseAgent_StringRefPreset(t *testing.T) {
	data := []byte(`preset: opus-high
prompt:
  text: You are the planner.
`)
	a, err := parseAgent(data)
	require.NoError(t, err)
	require.Equal(t, "opus-high", a.PresetName)
	require.Nil(t, a.PresetInline)
	require.Equal(t, "You are the planner.", a.Prompt.Text)
	require.Empty(t, a.Prompt.File)
}

func TestParseAgent_InlinePreset(t *testing.T) {
	data := []byte(`preset:
  adapter: codex
  model: gpt-5.5
  reasoning: high
  provider: openai
prompt:
  text: You are an extractor.
`)
	a, err := parseAgent(data)
	require.NoError(t, err)
	require.Empty(t, a.PresetName)
	require.NotNil(t, a.PresetInline)
	require.Equal(t, "codex", a.PresetInline.Adapter)
	require.Equal(t, "gpt-5.5", a.PresetInline.Model)
	require.Equal(t, "high", a.PresetInline.Reasoning)
	require.Equal(t, "openai", a.PresetInline.Provider)
}

func TestParseAgent_InlinePresetMissingRequired(t *testing.T) {
	// adapter only, no model
	data := []byte(`preset:
  adapter: codex
prompt:
  text: hi
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "adapter and model")
}

func TestParseAgent_PromptText(t *testing.T) {
	data := []byte(`preset: opus-high
prompt:
  text: |
    Multi-line
    prompt body.
`)
	a, err := parseAgent(data)
	require.NoError(t, err)
	require.Contains(t, a.Prompt.Text, "Multi-line")
	require.Contains(t, a.Prompt.Text, "prompt body.")
}

func TestLoadByName_PromptFileExists(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	// Drop a prompt file under .subtask/prompts/.
	promptsDir := filepath.Join(env.RootDir, ".subtask", "prompts")
	require.NoError(t, os.MkdirAll(promptsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(promptsDir, "planner.md"), []byte("Plan carefully."), 0o644))

	// Drop the agent file referencing it.
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(
		`preset: opus-high
prompt:
  file: prompts/planner.md
`), 0o644))

	a, err := LoadByName("planner")
	require.NoError(t, err)
	require.Equal(t, "planner", a.Name)
	require.Equal(t, "prompts/planner.md", a.Prompt.File)
	require.Empty(t, a.Prompt.Text)

	body, err := a.ResolvePromptText()
	require.NoError(t, err)
	require.Equal(t, "Plan carefully.", body)
}

func TestLoadByName_PromptFileMissing(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "broken.yaml"), []byte(
		`preset: opus-high
prompt:
  file: prompts/does-not-exist.md
`), 0o644))

	_, err := LoadByName("broken")
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompts/does-not-exist.md")
	require.Contains(t, err.Error(), "not found")
}

func TestParseAgent_PromptTextEmptyString(t *testing.T) {
	// prompt: { text: "" } passes the "exactly one source" check (text is
	// explicitly set) but is meaningless as a role prompt. Reject at load
	// time so the failure surfaces at draft, not at the next send.
	data := []byte(`preset: opus-high
prompt:
  text: ""
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt.text")
	require.Contains(t, err.Error(), "empty")
}

func TestParseAgent_PromptTextWhitespaceOnly(t *testing.T) {
	// Whitespace-only text would BuildPrompt to an empty ## Agent block —
	// equivalent to no prompt at all. Reject early.
	data := []byte("preset: opus-high\nprompt:\n  text: \"   \\n  \"\n")
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt.text")
	require.Contains(t, err.Error(), "whitespace")
}

func TestParseAgent_PromptFileEmptyString(t *testing.T) {
	data := []byte(`preset: opus-high
prompt:
  file: ""
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt.file")
	require.Contains(t, err.Error(), "empty")
}

func TestParseAgent_PromptFileWhitespaceOnly(t *testing.T) {
	data := []byte("preset: opus-high\nprompt:\n  file: \"   \"\n")
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt.file")
}

func TestParseAgent_PromptBothTextAndFile(t *testing.T) {
	data := []byte(`preset: opus-high
prompt:
  text: inline
  file: prompts/x.md
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mutually exclusive")
}

func TestParseAgent_PromptNeitherTextNorFile(t *testing.T) {
	data := []byte(`preset: opus-high
prompt: {}
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt")
}

func TestParseAgent_PromptSkillIsDeferred(t *testing.T) {
	data := []byte(`preset: opus-high
prompt:
  skill: org-jira-extract
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "skill")
	require.Contains(t, err.Error(), "not yet supported")
}

func TestParseAgent_MissingPreset(t *testing.T) {
	data := []byte(`prompt:
  text: hi
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "preset")
}

func TestParseAgent_MissingPrompt(t *testing.T) {
	data := []byte(`preset: opus-high
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt")
}

func TestLoadByName_FileNotFound(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	_, err := LoadByName("ghost")
	require.Error(t, err)
	require.Contains(t, err.Error(), ".subtask/agents/ghost.yaml")
	require.Contains(t, err.Error(), "not found")
}

// --- Path-traversal hardening (P1 #2) -----------------------------------

func TestLoadByName_RejectsTraversalInName(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	_, err := LoadByName("../../etc/passwd")
	require.Error(t, err)
	require.Contains(t, err.Error(), "path separators")
}

func TestLoadByName_RejectsAbsoluteName(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	_, err := LoadByName("/etc/passwd")
	require.Error(t, err)
	// On Unix this trips the "absolute path" branch; the separator branch
	// would have matched anyway, but we want the absolute-specific message.
	require.True(t,
		strings.Contains(err.Error(), "absolute path") || strings.Contains(err.Error(), "path separators"),
		"got: %v", err)
}

func TestLoadByName_RejectsDotDotName(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	_, err := LoadByName("..")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not allowed")
}

func TestLoadByName_RejectsAbsolutePromptFile(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "abs.yaml"), []byte(
		`preset: opus-high
prompt:
  file: /etc/passwd
`), 0o644))

	_, err := LoadByName("abs")
	require.Error(t, err)
	require.Contains(t, err.Error(), "absolute")
}

func TestLoadByName_RejectsDotDotPromptFile(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)
	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "esc.yaml"), []byte(
		`preset: opus-high
prompt:
  file: ../../../etc/passwd
`), 0o644))

	_, err := LoadByName("esc")
	require.Error(t, err)
	require.Contains(t, err.Error(), "traversal")
}

func TestLoadByName_AcceptsPromptFileUnderPromptsDir(t *testing.T) {
	// Positive case: a normal nested path under .subtask/prompts/ resolves
	// and loads. Confirms the traversal check doesn't over-reject.
	env := testutil.NewTestEnv(t, 0)

	promptsDir := filepath.Join(env.RootDir, ".subtask", "prompts")
	require.NoError(t, os.MkdirAll(promptsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(promptsDir, "planner.md"), []byte("Plan."), 0o644))

	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(
		`preset: opus-high
prompt:
  file: prompts/planner.md
`), 0o644))

	a, err := LoadByName("planner")
	require.NoError(t, err)
	body, err := a.ResolvePromptText()
	require.NoError(t, err)
	require.Equal(t, "Plan.", body)
}
