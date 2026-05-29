package workspace

import (
	"fmt"
	"slices"
	"strings"

	"github.com/kgruel/subtask/pkg/task"
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

func ResolveAdapter(cfg *Config, t *task.Task, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	if t != nil && strings.TrimSpace(t.Adapter) != "" {
		return strings.TrimSpace(t.Adapter)
	}
	if cfg != nil && strings.TrimSpace(cfg.Adapter) != "" {
		return strings.TrimSpace(cfg.Adapter)
	}
	return ""
}

func ResolveProvider(cfg *Config, t *task.Task, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	if t != nil && strings.TrimSpace(t.Provider) != "" {
		return strings.TrimSpace(t.Provider)
	}
	if cfg != nil && strings.TrimSpace(cfg.Provider) != "" {
		return strings.TrimSpace(cfg.Provider)
	}
	return ""
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

// ConfigWithOverrides returns a copy of cfg with each non-empty override
// applied. Empty overrides preserve the existing value (set-or-preserve),
// matching ApplyAgentSpec's overlay semantics. Callers pass already-resolved
// bundles, so an empty field means "no override here", never "clear it".
func ConfigWithOverrides(cfg *Config, adapter, provider, model, reasoning string) *Config {
	if cfg == nil {
		return nil
	}
	cp := *cfg

	if adapter = strings.TrimSpace(adapter); adapter != "" {
		cp.Adapter = adapter
	}
	if provider = strings.TrimSpace(provider); provider != "" {
		cp.Provider = provider
	}
	if model = strings.TrimSpace(model); model != "" {
		cp.Model = model
	}
	if reasoning = strings.TrimSpace(reasoning); reasoning != "" {
		cp.Reasoning = reasoning
	}

	return &cp
}
