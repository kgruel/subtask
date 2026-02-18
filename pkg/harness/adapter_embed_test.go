package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBuiltinAdapter_Claude(t *testing.T) {
	cfg, err := LoadBuiltinAdapter("claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "claude" {
		t.Errorf("Name = %q, want %q", cfg.Name, "claude")
	}
	if cfg.CLI != "claude" {
		t.Errorf("CLI = %q, want %q", cfg.CLI, "claude")
	}
	if cfg.OutputParser != "claude" {
		t.Errorf("OutputParser = %q, want %q", cfg.OutputParser, "claude")
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
	if cfg.PromptVia != "arg" {
		t.Errorf("PromptVia = %q, want %q", cfg.PromptVia, "arg")
	}
}

func TestLoadBuiltinAdapter_Codex(t *testing.T) {
	cfg, err := LoadBuiltinAdapter("codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "codex" {
		t.Errorf("Name = %q, want %q", cfg.Name, "codex")
	}
	if cfg.CLI != "codex" {
		t.Errorf("CLI = %q, want %q", cfg.CLI, "codex")
	}
	if cfg.OutputParser != "codex" {
		t.Errorf("OutputParser = %q, want %q", cfg.OutputParser, "codex")
	}
	if !cfg.Capabilities.ContinueSession {
		t.Error("Capabilities.ContinueSession = false, want true")
	}
	if !cfg.Capabilities.Review {
		t.Error("Capabilities.Review = false, want true")
	}
	if cfg.SessionHandler != "codex" {
		t.Errorf("SessionHandler = %q, want %q", cfg.SessionHandler, "codex")
	}
}

func TestLoadBuiltinAdapter_OpenCode(t *testing.T) {
	cfg, err := LoadBuiltinAdapter("opencode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "opencode" {
		t.Errorf("Name = %q, want %q", cfg.Name, "opencode")
	}
	if cfg.CLI != "opencode" {
		t.Errorf("CLI = %q, want %q", cfg.CLI, "opencode")
	}
	if cfg.PromptVia != "stdin" {
		t.Errorf("PromptVia = %q, want %q", cfg.PromptVia, "stdin")
	}
	if cfg.OutputParser != "opencode" {
		t.Errorf("OutputParser = %q, want %q", cfg.OutputParser, "opencode")
	}
	if cfg.SessionHandler != "none" {
		t.Errorf("SessionHandler = %q, want %q", cfg.SessionHandler, "none")
	}
}

func TestLoadBuiltinAdapter_NotFound(t *testing.T) {
	_, err := LoadBuiltinAdapter("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent adapter, got nil")
	}
}

func TestLoadAdapter_UserOverride(t *testing.T) {
	dir := t.TempDir()

	// Write a user override that shadows the built-in claude adapter.
	content := `
name: claude
cli: my-custom-claude
args:
  - "--custom"
output_parser: text
`
	if err := os.WriteFile(filepath.Join(dir, "claude.yaml"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadAdapter(dir, "claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should use the override, not the built-in.
	if cfg.CLI != "my-custom-claude" {
		t.Errorf("CLI = %q, want %q (user override)", cfg.CLI, "my-custom-claude")
	}
}

func TestLoadAdapter_FallbackToBuiltin(t *testing.T) {
	dir := t.TempDir() // empty directory, no overrides

	cfg, err := LoadAdapter(dir, "codex")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should fall back to built-in codex.
	if cfg.CLI != "codex" {
		t.Errorf("CLI = %q, want %q (built-in fallback)", cfg.CLI, "codex")
	}
}

func TestLoadAdapter_EmptyUserDir(t *testing.T) {
	cfg, err := LoadAdapter("", "claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "claude" {
		t.Errorf("Name = %q, want %q", cfg.Name, "claude")
	}
}
