package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/testutil"
)

func TestParseAgent_FlatDispatch(t *testing.T) {
	data := []byte(`adapter: claude
model: opus
reasoning: high
prompt:
  text: You are the planner.
`)
	a, err := parseAgent(data)
	require.NoError(t, err)
	require.Equal(t, "claude", a.Adapter)
	require.Equal(t, "opus", a.Model)
	require.Equal(t, "high", a.Reasoning)
	require.Equal(t, "You are the planner.", a.Prompt.Text)
	require.Empty(t, a.Prompt.File)
}

func TestParseAgent_AllFields(t *testing.T) {
	data := []byte(`adapter: codex
model: gpt-5.5
reasoning: high
provider: openai
prompt:
  text: You are an extractor.
`)
	a, err := parseAgent(data)
	require.NoError(t, err)
	require.Equal(t, "codex", a.Adapter)
	require.Equal(t, "gpt-5.5", a.Model)
	require.Equal(t, "high", a.Reasoning)
	require.Equal(t, "openai", a.Provider)
}

func TestParseAgent_BareDispatch(t *testing.T) {
	// No prompt: block — bare-dispatch agent (no role injected).
	data := []byte(`adapter: claude
model: sonnet
`)
	a, err := parseAgent(data)
	require.NoError(t, err)
	require.Equal(t, "claude", a.Adapter)
	require.Equal(t, "sonnet", a.Model)
	require.Empty(t, a.Prompt.Text)
	require.Empty(t, a.Prompt.File)
}

func TestParseAgent_MissingAdapter(t *testing.T) {
	data := []byte(`model: opus
prompt:
  text: hi
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "adapter")
}

func TestParseAgent_MissingModel(t *testing.T) {
	data := []byte(`adapter: claude
prompt:
  text: hi
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "model")
}

func TestParseAgent_PromptText(t *testing.T) {
	data := []byte(`adapter: claude
model: opus
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

	promptsDir := filepath.Join(env.RootDir, ".subtask", "prompts")
	require.NoError(t, os.MkdirAll(promptsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(promptsDir, "planner.md"), []byte("Plan carefully."), 0o644))

	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(
		`adapter: claude
model: opus
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
		`adapter: claude
model: opus
prompt:
  file: prompts/does-not-exist.md
`), 0o644))

	_, err := LoadByName("broken")
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompts/does-not-exist.md")
	require.Contains(t, err.Error(), "not found")
}

func TestParseAgent_PromptTextEmptyString(t *testing.T) {
	data := []byte(`adapter: claude
model: opus
prompt:
  text: ""
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt.text")
	require.Contains(t, err.Error(), "empty")
}

func TestParseAgent_PromptTextWhitespaceOnly(t *testing.T) {
	data := []byte("adapter: claude\nmodel: opus\nprompt:\n  text: \"   \\n  \"\n")
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt.text")
	require.Contains(t, err.Error(), "whitespace")
}

func TestParseAgent_PromptFileEmptyString(t *testing.T) {
	data := []byte(`adapter: claude
model: opus
prompt:
  file: ""
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt.file")
	require.Contains(t, err.Error(), "empty")
}

func TestParseAgent_PromptFileWhitespaceOnly(t *testing.T) {
	data := []byte("adapter: claude\nmodel: opus\nprompt:\n  file: \"   \"\n")
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt.file")
}

func TestParseAgent_PromptBothTextAndFile(t *testing.T) {
	data := []byte(`adapter: claude
model: opus
prompt:
  text: inline
  file: prompts/x.md
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "mutually exclusive")
}

func TestParseAgent_PromptNeitherTextNorFile(t *testing.T) {
	data := []byte(`adapter: claude
model: opus
prompt: {}
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "prompt")
}

func TestParseAgent_PromptSkillIsDeferred(t *testing.T) {
	data := []byte(`adapter: claude
model: opus
prompt:
  skill: org-jira-extract
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "skill")
	require.Contains(t, err.Error(), "not yet supported")
}

func TestParseAgent_UnknownTopLevelKey(t *testing.T) {
	// A typo like 'promt:' must surface as an error, not silently produce
	// a bare-dispatch agent.
	data := []byte(`adapter: claude
model: opus
promt:
  text: You are the planner.
`)
	_, err := parseAgent(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "promt")
}

func TestParseAgent_BareDispatchNoPromptFieldIsOK(t *testing.T) {
	// Omitting prompt: entirely is the bare-dispatch case — must succeed
	// even with strict decoding.
	data := []byte(`adapter: claude
model: sonnet
`)
	a, err := parseAgent(data)
	require.NoError(t, err)
	require.Equal(t, "claude", a.Adapter)
	require.Empty(t, a.Prompt.Text)
	require.Empty(t, a.Prompt.File)
}

func TestLoadByName_FileNotFound(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)

	_, err := LoadByName("ghost")
	require.Error(t, err)
	require.Contains(t, err.Error(), ".subtask/agents/ghost.yaml")
	require.Contains(t, err.Error(), "not found")
}

// --- Path-traversal hardening -------------------------------------------------

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
		`adapter: claude
model: opus
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
		`adapter: claude
model: opus
prompt:
  file: ../../../etc/passwd
`), 0o644))

	_, err := LoadByName("esc")
	require.Error(t, err)
	require.Contains(t, err.Error(), "traversal")
}

func TestLoadByName_AcceptsPromptFileUnderPromptsDir(t *testing.T) {
	env := testutil.NewTestEnv(t, 0)

	promptsDir := filepath.Join(env.RootDir, ".subtask", "prompts")
	require.NoError(t, os.MkdirAll(promptsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(promptsDir, "planner.md"), []byte("Plan."), 0o644))

	agentsDir := filepath.Join(env.RootDir, ".subtask", "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "planner.yaml"), []byte(
		`adapter: claude
model: opus
prompt:
  file: prompts/planner.md
`), 0o644))

	a, err := LoadByName("planner")
	require.NoError(t, err)
	body, err := a.ResolvePromptText()
	require.NoError(t, err)
	require.Equal(t, "Plan.", body)
}
