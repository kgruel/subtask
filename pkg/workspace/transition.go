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
	// AgentName is the from-stage's bound agent label, used only for the
	// event payload's from_agent field. Empty when none.
	AgentName string
	// NoOp is true when the resolved from-stage equals toStage (the task
	// was already on the requested step). No history event is written and
	// no adapter swap is performed. Callers should print "already on step
	// X" rather than a transition header.
	NoOp bool
}

// ApplyStageTransition swaps the task's harness on a stage/step
// transition.
//
// Single point of truth for adapter swap + session-clear + history
// event semantics. Shared by cmd/subtask/stage.go (workflow flavor) and
// pkg/routine/runner.go (routine flavor); each caller supplies a
// resolveFrom closure that maps the raw history.Tail().Stage observed
// inside the lock into the from-stage name + from-agent name to
// record.
//
// Inputs are pre-resolved by the caller — agent lookup happens at the
// call site because workflow and routine resolve agents differently
// (workflow stage names a cfg agent; a routine step may name a cfg
// agent OR carry an inline agent spec).
//
// Behavior:
//   - Reads the current stage from history.Tail INSIDE the lock. This
//     prevents two concurrent transitions from observing the same
//     stale fromStage; the second transition's `from` correctly
//     reflects the first's `to`. Without the inside-lock read, the
//     payload would record an obsolete from value and (worse) the
//     from-agent → to-agent adapter-swap decision could be wrong.
//   - If the resolved from-stage equals toStage, sets FromState.NoOp =
//     true and returns without writing any history or swapping adapters.
//     The check happens under the lock so callers observe the actual
//     current step, not a stale pre-lock read.
//   - If toAgentSpec is non-nil and changes the adapter, overlay its
//     non-empty fields onto TASK.md and clear state.SessionID — a
//     fresh session on the new adapter reads the workspace, PLAN.md,
//     and PROGRESS.json for cross-stage context (file-based
//     collaboration; design principle #5).
//   - Otherwise appends a stage.changed history event.
//
// Returns the FromState the helper observed/resolved so callers can
// render output without re-reading history outside the lock.
func ApplyStageTransition(
	taskName string,
	toStage string,
	toAgentName string,
	toAgentSpec *AgentSpec,
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

		// No-op: task is already on the requested step (observed under lock).
		// Skip all writes so callers can print "already on step X" without
		// writing a spurious stage.changed event.
		if from.Stage == toStage {
			from.NoOp = true
			return nil
		}

		state, _ := task.LoadState(taskName)

		if toAgentSpec != nil && (toAgentSpec.Adapter != "" || toAgentSpec.Model != "" || toAgentSpec.Reasoning != "" || toAgentSpec.Provider != "") {
			t, err := task.Load(taskName)
			if err != nil {
				return err
			}
			oldAdapter := t.Adapter
			if toAgentSpec.Adapter != "" {
				t.Adapter = toAgentSpec.Adapter
			}
			if toAgentSpec.Provider != "" {
				t.Provider = toAgentSpec.Provider
			}
			if toAgentSpec.Model != "" {
				t.Model = toAgentSpec.Model
			}
			if toAgentSpec.Reasoning != "" {
				t.Reasoning = toAgentSpec.Reasoning
			}
			if err := t.Save(); err != nil {
				return fmt.Errorf("failed to save task after harness swap: %w", err)
			}

			if state != nil && toAgentSpec.Adapter != "" && toAgentSpec.Adapter != oldAdapter {
				state.SessionID = ""
				state.Adapter = toAgentSpec.Adapter
				if err := state.Save(taskName); err != nil {
					return fmt.Errorf("failed to clear session after adapter swap: %w", err)
				}
			}
		}

		data, _ := json.Marshal(map[string]any{
			"from":       from.Stage,
			"to":         toStage,
			"from_agent": from.AgentName,
			"to_agent":   toAgentName,
		})
		return history.AppendLocked(taskName, history.Event{TS: ts, Type: "stage.changed", Data: data})
	})
	return from, err
}
