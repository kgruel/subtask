package harness

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// AdapterConfig describes a worker backend in a declarative, YAML-driven format.
// Built-in adapters (claude, codex, opencode) ship as embedded YAMLs;
// users can override or add custom adapters by placing files in a config directory.
type AdapterConfig struct {
	Name           string            `yaml:"name"`
	CLI            string            `yaml:"cli"`
	Args           []string          `yaml:"args"`
	PromptVia      string            `yaml:"prompt_via"`      // "arg" (default) or "stdin"
	ContinueArgs   []string          `yaml:"continue_args"`   // inserted before prompt on continuation
	OutputParser   string            `yaml:"output_parser"`   // "claude", "codex", "opencode", "generic-jsonl", "text"
	Parse          AdapterParseRules `yaml:"parse"`
	Capabilities   AdapterCaps       `yaml:"capabilities"`
	SessionHandler string            `yaml:"session_handler"` // "claude", "codex", "none" (default)
	Env            map[string]string `yaml:"env,omitempty"`
}

// AdapterParseRules configures how the generic-jsonl parser extracts fields.
// Ignored when OutputParser names a dedicated parser (claude, codex, opencode).
type AdapterParseRules struct {
	SessionID       string            `yaml:"session_id"`
	Reply           string            `yaml:"reply"`
	ReplyAccumulate bool              `yaml:"reply_accumulate"`
	ToolCallMatch   map[string]string `yaml:"tool_call_match,omitempty"`
}

// AdapterCaps declares what the adapter supports.
type AdapterCaps struct {
	ContinueSession bool `yaml:"continue_session"`
	Review          bool `yaml:"review"`
}

// LoadAdapterConfigFromDir reads <dir>/<name>.yaml and returns the parsed config.
func LoadAdapterConfigFromDir(dir, name string) (*AdapterConfig, error) {
	path := filepath.Join(dir, name+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("adapter %q: %w", name, err)
	}
	return parseAdapterConfig(data)
}

// parseAdapterConfig unmarshals YAML bytes into an AdapterConfig,
// applies defaults, and validates required fields.
func parseAdapterConfig(data []byte) (*AdapterConfig, error) {
	var cfg AdapterConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid adapter config: %w", err)
	}

	// Defaults.
	if cfg.PromptVia == "" {
		cfg.PromptVia = "arg"
	}
	if cfg.OutputParser == "" {
		cfg.OutputParser = "text"
	}
	if cfg.SessionHandler == "" {
		cfg.SessionHandler = "none"
	}

	// Validation.
	if cfg.Name == "" {
		return nil, fmt.Errorf("adapter config: name is required")
	}
	if cfg.CLI == "" {
		return nil, fmt.Errorf("adapter config: cli is required")
	}

	return &cfg, nil
}
