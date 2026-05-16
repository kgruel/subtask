package workspace

import (
	"github.com/kgruel/subtask/pkg/task"
)

// ResolveOverrides holds per-call flag values passed to Resolve.
type ResolveOverrides struct {
	Adapter   string
	Provider  string
	Model     string
	Reasoning string
	// Agent is a pre-loaded agent spec to overlay (caller loads with agent.LoadByName
	// to avoid a circular import). Nil means no --agent override.
	Agent *AgentSpec
}

// Resolved holds the effective adapter/provider/model/reasoning after agent
// overlay, snapshot fallback, and project-default fallback.
type Resolved struct {
	Adapter   string
	Provider  string
	Model     string
	Reasoning string
}

// ApplyAgentSpec overlays non-empty agent spec fields onto the flag pointers
// without overwriting values already set by the caller.
func ApplyAgentSpec(p AgentSpec, adapter, provider, model, reasoning *string) {
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

// Resolve combines agent overlay, snapshot/config fallback, and reasoning
// validation into one call. Returns an error if the resolved reasoning is
// invalid for the resolved adapter.
func Resolve(cfg *Config, t *task.Task, o ResolveOverrides) (Resolved, error) {
	adapterFlag := o.Adapter
	providerFlag := o.Provider
	modelFlag := o.Model
	reasoningFlag := o.Reasoning

	if o.Agent != nil {
		ApplyAgentSpec(*o.Agent, &adapterFlag, &providerFlag, &modelFlag, &reasoningFlag)
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
