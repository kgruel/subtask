package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAdapterConfig_AllFields(t *testing.T) {
	yaml := `
name: claude
cli: claude
args:
  - "--print"
  - "--verbose"
prompt_via: stdin
continue_args:
  - "--resume"
  - "{{session_id}}"
output_parser: claude
parse:
  session_id: ".session_id"
  reply: ".result"
  reply_accumulate: true
  tool_call_match:
    type: tool_use
capabilities:
  continue_session: true
  review: true
session_handler: claude
env:
  FOO: bar
  BAZ: qux
`
	cfg, err := parseAdapterConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Name != "claude" {
		t.Errorf("Name = %q, want %q", cfg.Name, "claude")
	}
	if cfg.CLI != "claude" {
		t.Errorf("CLI = %q, want %q", cfg.CLI, "claude")
	}
	if len(cfg.Args) != 2 || cfg.Args[0] != "--print" || cfg.Args[1] != "--verbose" {
		t.Errorf("Args = %v, want [--print --verbose]", cfg.Args)
	}
	if cfg.PromptVia != "stdin" {
		t.Errorf("PromptVia = %q, want %q", cfg.PromptVia, "stdin")
	}
	if len(cfg.ContinueArgs) != 2 || cfg.ContinueArgs[0] != "--resume" {
		t.Errorf("ContinueArgs = %v, want [--resume {{session_id}}]", cfg.ContinueArgs)
	}
	if cfg.OutputParser != "claude" {
		t.Errorf("OutputParser = %q, want %q", cfg.OutputParser, "claude")
	}
	if cfg.Parse.SessionID != ".session_id" {
		t.Errorf("Parse.SessionID = %q, want %q", cfg.Parse.SessionID, ".session_id")
	}
	if cfg.Parse.Reply != ".result" {
		t.Errorf("Parse.Reply = %q, want %q", cfg.Parse.Reply, ".result")
	}
	if !cfg.Parse.ReplyAccumulate {
		t.Error("Parse.ReplyAccumulate = false, want true")
	}
	if cfg.Parse.ToolCallMatch["type"] != "tool_use" {
		t.Errorf("Parse.ToolCallMatch = %v, want map[type:tool_use]", cfg.Parse.ToolCallMatch)
	}
	if !cfg.Capabilities.ContinueSession {
		t.Error("Capabilities.ContinueSession = false, want true")
	}
	if !cfg.Capabilities.Review {
		t.Error("Capabilities.Review = false, want true")
	}
	if cfg.SessionHandler != "claude" {
		t.Errorf("SessionHandler = %q, want %q", cfg.SessionHandler, "claude")
	}
	if cfg.Env["FOO"] != "bar" || cfg.Env["BAZ"] != "qux" {
		t.Errorf("Env = %v, want map[FOO:bar BAZ:qux]", cfg.Env)
	}
}

func TestParseAdapterConfig_Defaults(t *testing.T) {
	yaml := `
name: minimal
cli: my-tool
`
	cfg, err := parseAdapterConfig([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.PromptVia != "arg" {
		t.Errorf("PromptVia default = %q, want %q", cfg.PromptVia, "arg")
	}
	if cfg.OutputParser != "text" {
		t.Errorf("OutputParser default = %q, want %q", cfg.OutputParser, "text")
	}
	if cfg.SessionHandler != "none" {
		t.Errorf("SessionHandler default = %q, want %q", cfg.SessionHandler, "none")
	}
}

func TestParseAdapterConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "missing name",
			yaml: "cli: tool\n",
		},
		{
			name: "missing cli",
			yaml: "name: tool\n",
		},
		{
			name: "both missing",
			yaml: "args: []\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAdapterConfig([]byte(tt.yaml))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestLoadAdapterConfigFromDir(t *testing.T) {
	dir := t.TempDir()

	// Write a valid adapter YAML.
	content := `
name: test-adapter
cli: test-cli
args:
  - "--flag"
output_parser: codex
`
	if err := os.WriteFile(filepath.Join(dir, "test-adapter.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadAdapterConfigFromDir(dir, "test-adapter")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "test-adapter" {
		t.Errorf("Name = %q, want %q", cfg.Name, "test-adapter")
	}
	if cfg.CLI != "test-cli" {
		t.Errorf("CLI = %q, want %q", cfg.CLI, "test-cli")
	}
	if cfg.OutputParser != "codex" {
		t.Errorf("OutputParser = %q, want %q", cfg.OutputParser, "codex")
	}
	// Defaults should be applied.
	if cfg.PromptVia != "arg" {
		t.Errorf("PromptVia default = %q, want %q", cfg.PromptVia, "arg")
	}
}

func TestLoadAdapterConfigFromDir_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadAdapterConfigFromDir(dir, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent adapter, got nil")
	}
}
