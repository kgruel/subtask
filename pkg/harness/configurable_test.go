package harness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigurableAdapter_TemplateArgs(t *testing.T) {
	cfg := &AdapterConfig{
		Name:      "test",
		CLI:       "test-cli",
		Args:      []string{"exec", "--json", "-m", "{{model}}"},
		PromptVia: "arg",
		ContinueArgs: []string{"resume", "{{session_id}}"},
	}
	vars := templateVars{
		Model: "gpt-4",
	}

	a, err := NewConfigurableAdapter(cfg, vars)
	require.NoError(t, err)

	// Fresh run (no continuation).
	args := a.buildArgs("do something", false)
	require.Equal(t, []string{"exec", "--json", "-m", "gpt-4", "do something"}, args)

	// Continuation run.
	vars.SessionID = "sess-42"
	a.vars = vars
	args = a.buildArgs("continue work", true)
	require.Equal(t, []string{"exec", "--json", "-m", "gpt-4", "resume", "sess-42", "continue work"}, args)
}

func TestConfigurableAdapter_TemplateArgs_EmptyModel(t *testing.T) {
	cfg := &AdapterConfig{
		Name:      "test",
		CLI:       "test-cli",
		Args:      []string{"exec", "--model", "{{model}}", "--flag"},
		PromptVia: "arg",
	}
	vars := templateVars{
		Model: "", // empty model
	}

	a, err := NewConfigurableAdapter(cfg, vars)
	require.NoError(t, err)

	args := a.buildArgs("hello", false)
	// --model and {{model}} should both be skipped; --flag stays.
	require.Equal(t, []string{"exec", "--flag", "hello"}, args)
}

func TestConfigurableAdapter_TemplateArgs_Stdin(t *testing.T) {
	cfg := &AdapterConfig{
		Name:      "test",
		CLI:       "test-cli",
		Args:      []string{"run", "--format", "json"},
		PromptVia: "stdin",
	}
	vars := templateVars{}

	a, err := NewConfigurableAdapter(cfg, vars)
	require.NoError(t, err)

	args := a.buildArgs("my prompt", false)
	// Prompt should NOT be appended when prompt_via=stdin.
	require.Equal(t, []string{"run", "--format", "json"}, args)
}

func TestConfigurableAdapter_TemplateArgs_PromptInArgs(t *testing.T) {
	cfg := &AdapterConfig{
		Name:      "test",
		CLI:       "aider",
		Args:      []string{"--message", "{{prompt}}"},
		PromptVia: "arg",
	}
	vars := templateVars{}

	a, err := NewConfigurableAdapter(cfg, vars)
	require.NoError(t, err)

	args := a.buildArgs("fix the bug", false)
	// {{prompt}} is in the args, so prompt should NOT be appended again.
	require.Equal(t, []string{"--message", "fix the bug"}, args)
}

func TestConfigurableAdapter_Review_Unsupported(t *testing.T) {
	cfg := &AdapterConfig{
		Name: "test",
		CLI:  "test-cli",
		Capabilities: AdapterCaps{
			Review: false,
		},
	}
	vars := templateVars{}

	a, err := NewConfigurableAdapter(cfg, vars)
	require.NoError(t, err)

	_, err = a.Review("/tmp", ReviewTarget{Uncommitted: true}, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "review")
}

func TestConfigurableAdapter_TemplateArgs_ContinueArgsTemplated(t *testing.T) {
	cfg := &AdapterConfig{
		Name:         "test",
		CLI:          "test-cli",
		Args:         []string{"exec"},
		PromptVia:    "arg",
		ContinueArgs: []string{"--resume", "{{session_id}}"},
	}
	vars := templateVars{
		SessionID: "s-99",
	}

	a, err := NewConfigurableAdapter(cfg, vars)
	require.NoError(t, err)

	args := a.buildArgs("follow up", true)
	require.Equal(t, []string{"exec", "--resume", "s-99", "follow up"}, args)
}

func TestConfigurableAdapter_TemplateArgs_AllVars(t *testing.T) {
	cfg := &AdapterConfig{
		Name: "test",
		CLI:  "test-cli",
		Args: []string{
			"--model", "{{model}}",
			"--reasoning", "{{reasoning}}",
			"--permission-mode", "{{permission_mode}}",
			"--tools", "{{tools}}",
			"--variant", "{{variant}}",
			"--agent", "{{agent}}",
		},
		PromptVia: "arg",
	}
	vars := templateVars{
		Model:          "opus",
		Reasoning:      "high",
		PermissionMode: "bypassPermissions",
		Tools:          "all",
		Variant:        "fast",
		Agent:          "coder",
	}

	a, err := NewConfigurableAdapter(cfg, vars)
	require.NoError(t, err)

	args := a.buildArgs("prompt", false)
	require.Equal(t, []string{
		"--model", "opus",
		"--reasoning", "high",
		"--permission-mode", "bypassPermissions",
		"--tools", "all",
		"--variant", "fast",
		"--agent", "coder",
		"prompt",
	}, args)
}

func TestConfigurableAdapter_TemplateArgs_MultipleEmptyVars(t *testing.T) {
	cfg := &AdapterConfig{
		Name: "test",
		CLI:  "test-cli",
		Args: []string{
			"exec",
			"--model", "{{model}}",
			"--reasoning", "{{reasoning}}",
			"--flag",
		},
		PromptVia: "arg",
	}
	vars := templateVars{
		Model:     "",
		Reasoning: "",
	}

	a, err := NewConfigurableAdapter(cfg, vars)
	require.NoError(t, err)

	args := a.buildArgs("hello", false)
	// Both --model/{{model}} and --reasoning/{{reasoning}} skipped.
	require.Equal(t, []string{"exec", "--flag", "hello"}, args)
}
