package workspace

import (
	"fmt"

	"github.com/kgruel/subtask/pkg/task"
)

// ResolveOverrides holds per-call flag values passed to Resolve.
type ResolveOverrides struct {
	Adapter   string
	Provider  string
	Model     string
	Reasoning string
	Preset    string
}

// Resolved holds the effective adapter/provider/model/reasoning after preset
// overlay, snapshot fallback, and project-default fallback.
type Resolved struct {
	Adapter   string
	Provider  string
	Model     string
	Reasoning string
}

// ApplyPreset overlays non-empty preset fields onto the flag pointers without
// overwriting values already set by the caller.
func ApplyPreset(p Preset, adapter, provider, model, reasoning *string) {
	if *adapter == "" {
		*adapter = p.Adapter
	}
	if *provider == "" {
		*provider = p.Provider
	}
	if *model == "" {
		*model = p.Model
	}
	if *reasoning == "" {
		*reasoning = p.Reasoning
	}
}

// PresetNames returns a sorted, comma-separated list of preset names in cfg,
// or "(none defined)" when the map is empty.
func PresetNames(cfg *Config) string {
	return joinKeys(cfg.Presets)
}

// Resolve combines preset overlay, snapshot/config fallback, and reasoning
// validation into one call. Returns an error if the preset is unknown or the
// resolved reasoning is invalid for the resolved adapter.
func Resolve(cfg *Config, t *task.Task, o ResolveOverrides) (Resolved, error) {
	adapterFlag := o.Adapter
	providerFlag := o.Provider
	modelFlag := o.Model
	reasoningFlag := o.Reasoning

	if o.Preset != "" {
		p, ok := cfg.Presets[o.Preset]
		if !ok {
			return Resolved{}, fmt.Errorf("unknown preset %q\n\nAvailable: %s", o.Preset, PresetNames(cfg))
		}
		ApplyPreset(p, &adapterFlag, &providerFlag, &modelFlag, &reasoningFlag)
	}

	r := Resolved{
		Adapter:   ResolveAdapter(cfg, t, adapterFlag),
		Provider:  ResolveProvider(cfg, t, providerFlag),
		Model:     ResolveModel(cfg, t, modelFlag),
		Reasoning: ResolveReasoning(cfg, t, reasoningFlag),
	}

	if err := ValidateReasoningFlag(r.Adapter, r.Reasoning); err != nil {
		return Resolved{}, err
	}

	return r, nil
}
