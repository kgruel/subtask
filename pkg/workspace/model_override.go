package workspace

import (
	"fmt"
	"slices"
	"strings"

	"github.com/zippoxer/subtask/pkg/task"
)

var validReasoningLevels = []string{"low", "medium", "high", "xhigh"}

func ValidateReasoningLevel(reasoning string) error {
	reasoning = strings.TrimSpace(reasoning)
	if reasoning == "" {
		return nil
	}
	if slices.Contains(validReasoningLevels, reasoning) {
		return nil
	}
	return fmt.Errorf("invalid reasoning %q\n\nAllowed: %s", reasoning, strings.Join(validReasoningLevels, ", "))
}

func ValidateReasoningFlag(adapterName, reasoning string) error {
	reasoning = strings.TrimSpace(reasoning)
	if reasoning == "" {
		return nil
	}
	if strings.TrimSpace(adapterName) != "codex" {
		return fmt.Errorf("reasoning is codex-only\n\nRemove --reasoning, or switch your adapter to codex with:\n  subtask config --user\nor (repo-only):\n  subtask config --project")
	}
	return ValidateReasoningLevel(reasoning)
}

func ResolveModel(cfg *Config, t *task.Task, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	if t != nil && strings.TrimSpace(t.Model) != "" {
		return strings.TrimSpace(t.Model)
	}
	if cfg != nil && strings.TrimSpace(cfg.Model) != "" {
		return strings.TrimSpace(cfg.Model)
	}
	return ""
}

func ResolveReasoning(cfg *Config, t *task.Task, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	if t != nil && strings.TrimSpace(t.Reasoning) != "" {
		return strings.TrimSpace(t.Reasoning)
	}
	if cfg != nil && strings.TrimSpace(cfg.Reasoning) != "" {
		return strings.TrimSpace(cfg.Reasoning)
	}
	return ""
}

func ConfigWithModelReasoning(cfg *Config, model, reasoning string) *Config {
	if cfg == nil {
		return nil
	}
	cp := *cfg

	model = strings.TrimSpace(model)
	if model != "" {
		cp.Model = model
	} else {
		cp.Model = ""
	}

	reasoning = strings.TrimSpace(reasoning)
	if reasoning != "" {
		cp.Reasoning = reasoning
	} else {
		cp.Reasoning = ""
	}

	return &cp
}
