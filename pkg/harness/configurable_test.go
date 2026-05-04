package harness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigurableAdapter_TemplateArgs(t *testing.T) {
	cfg := &AdapterConfig{
		Name:         "test",
		CLI:          "test-cli",
		Args:         []string{"exec", "--json", "-m", "{{model}}"},
		PromptVia:    "arg",
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

func TestConfigurableAdapter_TemplateArgs_CompoundArgWithEmptyVar(t *testing.T) {
	// codex.yaml uses "-c model_reasoning_effort={{reasoning}}" — a compound
	// arg form (key=value). When reasoning is empty, the whole arg AND the
	// preceding "-c" must be dropped, otherwise codex receives a malformed
	// "model_reasoning_effort=" override and errors.
	cfg := &AdapterConfig{
		Name: "test",
		CLI:  "test-cli",
		Args: []string{
			"exec",
			"-m", "{{model}}",
			"-c", "model_reasoning_effort={{reasoning}}",
			"--flag",
		},
		PromptVia: "arg",
	}

	t.Run("reasoning set", func(t *testing.T) {
		a, err := NewConfigurableAdapter(cfg, templateVars{Model: "gpt-5.5", Reasoning: "low"})
		require.NoError(t, err)
		args := a.buildArgs("hello", false)
		require.Equal(t, []string{
			"exec", "-m", "gpt-5.5",
			"-c", "model_reasoning_effort=low",
			"--flag", "hello",
		}, args)
	})

	t.Run("reasoning empty", func(t *testing.T) {
		a, err := NewConfigurableAdapter(cfg, templateVars{Model: "gpt-5.5", Reasoning: ""})
		require.NoError(t, err)
		args := a.buildArgs("hello", false)
		// "-c" and the compound arg both drop; -m/model survive.
		require.Equal(t, []string{
			"exec", "-m", "gpt-5.5",
			"--flag", "hello",
		}, args)
	})

	t.Run("multiple known vars one empty", func(t *testing.T) {
		// If a compound arg has multiple known vars and any is empty, drop.
		cfg2 := &AdapterConfig{
			Name: "test", CLI: "test-cli", PromptVia: "arg",
			Args: []string{"-c", "{{model}}-{{variant}}"},
		}
		a, err := NewConfigurableAdapter(cfg2, templateVars{Model: "m1", Variant: ""})
		require.NoError(t, err)
		args := a.buildArgs("hello", false)
		require.Equal(t, []string{"hello"}, args, "compound with one empty var drops the pair")
	})
}
