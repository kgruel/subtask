package workspace

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
)

// FromState is the from-stage information ApplyStageTransition resolved
// while holding the task lock. Returned so callers can render
// "from → to" headers without a second history read.
type FromState struct {
	// Stage is the from-stage name to write into the stage.changed
	// payload and to surface in user-facing output. Callers default the
	// raw tail.Stage to a workflow/routine entry point in the resolver
	// closure.
	Stage string
	// PresetName is the from-stage's bound preset, used only for the
	// event payload's from_preset field. Empty when none.
	PresetName string
}

// ApplyStageTransition swaps the task's harness on a stage/step
// transition.
//
// Single point of truth for adapter swap + session-clear + history
// event semantics. Shared by cmd/subtask/stage.go (workflow flavor) and
// pkg/routine/runner.go (routine flavor); each caller supplies a
// resolveFrom closure that maps the raw history.Tail().Stage observed
// inside the lock into the from-stage name + from-preset name to
// record.
//
// Inputs are pre-resolved by the caller — preset lookup happens at the
// call site because workflow and routine resolve presets differently
// (workflow stage names a cfg preset; a routine step may name a cfg
// preset OR carry an inline agent preset).
//
// Behavior:
//   - Reads the current stage from history.Tail INSIDE the lock. This
//     prevents two concurrent transitions from observing the same
//     stale fromStage; the second transition's `from` correctly
//     reflects the first's `to`. Without the inside-lock read, the
//     payload would record an obsolete from value and (worse) the
//     from-preset → to-preset adapter-swap decision could be wrong.
//   - If toPreset is non-nil and changes the adapter, overlay its
//     non-empty fields onto TASK.md and clear state.SessionID — a
//     fresh session on the new adapter reads the workspace, PLAN.md,
//     and PROGRESS.json for cross-stage context (file-based
//     collaboration; design principle #5).
//   - Always append a stage.changed history event.
//
// Returns the FromState the helper observed/resolved so callers can
// render output without re-reading history outside the lock.
func ApplyStageTransition(
	taskName string,
	toStage string,
	toPresetName string,
	toPreset *Preset,
	ts time.Time,
	resolveFrom func(rawFromStage string) FromState,
) (FromState, error) {
	var from FromState
	err := task.WithLock(taskName, func() error {
		tail, _ := history.Tail(taskName)
		raw := tail.Stage
		if resolveFrom != nil {
			from = resolveFrom(raw)
		} else {
			from = FromState{Stage: raw}
		}

		state, _ := task.LoadState(taskName)

		if toPreset != nil && (toPreset.Adapter != "" || toPreset.Model != "" || toPreset.Reasoning != "" || toPreset.Provider != "") {
			t, err := task.Load(taskName)
			if err != nil {
				return err
			}
			oldAdapter := t.Adapter
			if toPreset.Adapter != "" {
				t.Adapter = toPreset.Adapter
			}
			if toPreset.Provider != "" {
				t.Provider = toPreset.Provider
			}
			if toPreset.Model != "" {
				t.Model = toPreset.Model
			}
			if toPreset.Reasoning != "" {
				t.Reasoning = toPreset.Reasoning
			}
			if err := t.Save(); err != nil {
				return fmt.Errorf("failed to save task after harness swap: %w", err)
			}

			if state != nil && toPreset.Adapter != "" && toPreset.Adapter != oldAdapter {
				state.SessionID = ""
				state.Adapter = toPreset.Adapter
				if err := state.Save(taskName); err != nil {
					return fmt.Errorf("failed to clear session after adapter swap: %w", err)
				}
			}
		}

		data, _ := json.Marshal(map[string]any{
			"from":        from.Stage,
			"to":          toStage,
			"from_preset": from.PresetName,
			"to_preset":   toPresetName,
		})
		return history.AppendLocked(taskName, history.Event{TS: ts, Type: "stage.changed", Data: data})
	})
	return from, err
}
