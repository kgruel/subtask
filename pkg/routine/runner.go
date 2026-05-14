package routine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/kgruel/subtask/pkg/agent"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/workspace"
)

// AdvanceResult is what the runner returns to the orchestration layer.
//
// The runner cannot construct a SendCmd (cmd/subtask is unimportable here),
// so when a new step should auto-dispatch a worker round, it returns
// Dispatch=true and the caller (cmd/subtask/send.go) runs a fresh SendCmd.
type AdvanceResult struct {
	// NextStep is the id of the step the routine has advanced into.
	// Empty when no transition occurred (current step is terminal,
	// advance != "auto", last step, or branch evaluation declined).
	NextStep string

	// Dispatch is true when the new step is regular AND has either
	// agent: or worker_instructions: set. Caller should run a fresh
	// SendCmd. Gate and terminal steps never set this.
	Dispatch bool

	// DispatchPrompt is the synthetic prompt the caller should send to
	// the dispatched worker. Worker prompt assembly (## Agent block,
	// per-step worker_instructions) happens in harness.BuildPrompt.
	DispatchPrompt string
}

// HandleAutoAdvance processes auto-advance after a worker reply is
// committed to history. Mirrors the order of operations used by the
// workflow auto-advance hook in send.go (run after the cleanup block).
//
// Steps:
//  1. Look up current step. Missing → error.
//  2. If step.Advance != "auto" OR step.Kind == terminal: stop.
//  3. Compute next step (branches → artifact frontmatter, else
//     declaration order).
//  4. Resolve the new step's preset and apply via
//     workspace.ApplyStageTransition (single source of truth for
//     adapter-swap + session-clear semantics).
//  5. For surfaced gate / terminal steps, append routine.surfaced —
//     the unread substrate watches this event so the lead sees the
//     handoff even when no new worker.finished fires (gates don't
//     dispatch, terminals end the routine).
//  6. Decide auto-dispatch per the table in
//     docs/dev/_audit-skill-workflow-primitives.md (agent or
//     worker_instructions → dispatch; gate/terminal never dispatch).
func HandleAutoAdvance(taskName string, r *Routine, currentStepID string, cfg *workspace.Config, ts time.Time) (AdvanceResult, error) {
	if r == nil {
		return AdvanceResult{}, fmt.Errorf("routine is nil")
	}
	current := r.GetStep(currentStepID)
	if current == nil {
		return AdvanceResult{}, fmt.Errorf("routine %q: current step %q not found", r.Name, currentStepID)
	}
	if current.Advance != "auto" {
		return AdvanceResult{}, nil
	}
	if current.Kind == KindTerminal {
		return AdvanceResult{}, nil
	}

	nextID, err := pickNextStep(taskName, r, current)
	if err != nil {
		return AdvanceResult{}, err
	}
	if nextID == "" {
		return AdvanceResult{}, nil
	}
	next := r.GetStep(nextID)
	if next == nil {
		// Shouldn't happen — validateSteps catches dangling branches at
		// load. Defense in depth.
		return AdvanceResult{}, fmt.Errorf("routine %q: next step %q not found", r.Name, nextID)
	}

	toPreset, toPresetName, err := ResolveStepPreset(next, cfg)
	if err != nil {
		return AdvanceResult{}, err
	}

	// Resolve the from-step name + preset INSIDE the lock so concurrent
	// transitions (e.g. a `subtask stage` racing with this auto-advance)
	// can't both observe the same stale fromStage. The branch decision
	// above (which uses `current` from outside the lock) is a separate
	// concern — branch evaluation reads an artifact file, not history,
	// and is acceptably racy for v1 (the recursion cap bounds blast
	// radius if a routine misbehaves).
	resolveFrom := func(raw string) workspace.FromState {
		if raw == "" {
			raw = r.EntryStep()
		}
		preset := ""
		if s := r.GetStep(raw); s != nil {
			preset = StepPresetName(s)
		}
		return workspace.FromState{Stage: raw, PresetName: preset}
	}

	if _, err := workspace.ApplyStageTransition(taskName, next.ID, toPresetName, toPreset, ts, resolveFrom); err != nil {
		return AdvanceResult{}, err
	}

	// Surface event: fires when auto-advance lands on a gate OR
	// terminal step with surface: true (default). The unread substrate
	// watches this so the lead sees the handoff:
	//   - Gates don't auto-dispatch, so no follow-up worker.finished is
	//     coming; without this event the gate is silently invisible.
	//   - Terminals end the routine entirely; the lead needs to know
	//     to merge / close even if the prior reply was silent.
	// surface: false suppresses the event — the routine author opted
	// out of surfacing. The stage.changed event still records the
	// transition for audit.
	if (next.Kind == KindTerminal || next.Kind == KindGate) && next.IsSurfaced() {
		data, _ := json.Marshal(map[string]any{
			"step": next.ID,
			"kind": next.Kind,
		})
		_ = history.Append(taskName, history.Event{TS: ts, Type: "routine.surfaced", Data: data})
	}

	res := AdvanceResult{NextStep: next.ID}
	if next.Kind == "" && (next.Agent != "" || strings.TrimSpace(next.WorkerInstructions) != "") {
		res.Dispatch = true
		res.DispatchPrompt = fmt.Sprintf("Proceed with the %s step.", next.ID)
	}
	return res, nil
}

// pickNextStep evaluates branches if any, else declaration order.
func pickNextStep(taskName string, r *Routine, current *Step) (string, error) {
	if len(current.Branches) > 0 && strings.TrimSpace(current.Produces) != "" {
		taskDir := task.Dir(taskName)
		for _, b := range current.Branches {
			match, err := readArtifactBool(taskDir, current.Produces, b.Field)
			if err != nil {
				return "", err
			}
			if match {
				return b.To, nil
			}
		}
	}
	return r.nextInOrder(current.ID), nil
}

// StepPresetName returns the displayable preset name for a step, used
// in the stage.changed event payload's from_preset / to_preset fields.
// Agent-driven steps surface the agent reference rather than the
// underlying preset name (agents can carry inline presets without a
// name); preset-driven steps surface the preset name directly.
//
// Exported so cmd/subtask/stage.go (the routine `stage` handler) shares
// the same labeling as the auto-advance runner.
func StepPresetName(s *Step) string {
	if s == nil {
		return ""
	}
	if s.Agent != "" {
		return "agent:" + s.Agent
	}
	return s.Preset
}

// ResolveStepPreset returns the preset to apply on entry to step `s`,
// along with its displayable name.
//
//   - Agent-driven step: load the agent, resolve its preset (string ref
//     against cfg.Presets or inline block).
//   - Preset-driven step: look up cfg.Presets[s.Preset].
//   - Neither: nil (caller will skip the swap; task keeps its prior
//     adapter/model — same semantics as workflow stages without a
//     preset binding).
//
// Exported so cmd/subtask/stage.go can reuse the same resolution chain
// the runner uses for auto-advance — avoids duplicating the agent/preset
// lookup ladder in two places.
func ResolveStepPreset(s *Step, cfg *workspace.Config) (*workspace.Preset, string, error) {
	if s == nil {
		return nil, "", nil
	}
	if s.Agent != "" {
		ag, err := agent.LoadByName(s.Agent)
		if err != nil {
			return nil, "", fmt.Errorf("routine step %q: %w", s.ID, err)
		}
		if ag.PresetInline != nil {
			p := *ag.PresetInline
			return &p, "agent:" + s.Agent, nil
		}
		p, ok := cfg.Presets[ag.PresetName]
		if !ok {
			return nil, "", fmt.Errorf("routine step %q: agent %q references unknown preset %q\n\nAvailable: %s",
				s.ID, s.Agent, ag.PresetName, workspace.PresetNames(cfg))
		}
		return &p, "agent:" + s.Agent, nil
	}
	if s.Preset != "" {
		p, ok := cfg.Presets[s.Preset]
		if !ok {
			return nil, "", fmt.Errorf("routine step %q: unknown preset %q\n\nAvailable: %s",
				s.ID, s.Preset, workspace.PresetNames(cfg))
		}
		return &p, s.Preset, nil
	}
	return nil, "", nil
}

// readArtifactBool reads the produced artifact's YAML frontmatter and
// returns the bool value at `field`.
//
// Per Q4 (resolved in the pre-plan), missing artifact file / absent
// field / non-bool value all default to false — falls through to the
// next branch (or default-advance if no branch matches). Only a parse
// failure on existing frontmatter is a real error.
//
// Mirrors the frontmatter parser in pkg/task/task.go:Load — leading
// "---\n", body terminated by "\n---", YAML decode of the inner block.
func readArtifactBool(taskDir, artifactRelPath, field string) (bool, error) {
	if artifactRelPath == "" || field == "" {
		return false, nil
	}
	p := filepath.Join(taskDir, artifactRelPath)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		// No frontmatter → no flags to read; default-advance.
		return false, nil
	}
	end := strings.Index(content[4:], "\n---")
	if end == -1 {
		// Malformed frontmatter (unterminated) is a real error — the
		// producer step intended a parseable artifact and shipped something
		// the loopback predicate can't read. Surface it.
		return false, fmt.Errorf("artifact %q: unclosed YAML frontmatter", artifactRelPath)
	}
	fmData := content[4 : 4+end]
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(fmData), &fm); err != nil {
		return false, fmt.Errorf("artifact %q: invalid frontmatter: %w", artifactRelPath, err)
	}
	v, ok := fm[field]
	if !ok {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, nil
	}
	return b, nil
}
