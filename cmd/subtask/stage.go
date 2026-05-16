package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/render"
	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/migrate"
	"github.com/kgruel/subtask/pkg/task/store"
	"github.com/kgruel/subtask/pkg/workspace"
)

// StageCmd implements 'subtask stage'.
type StageCmd struct {
	Task   string `arg:"" help:"Task name"`
	Stage  string `arg:"" help:"Stage to set"`
	Prompt string `arg:"" optional:"" help:"Extra user message sent alongside the new stage's worker_instructions (or alone if there are none)"`
	NoSend bool   `name:"no-send" help:"Skip auto-dispatch even if the new stage has worker_instructions"`
	Quiet  bool   `short:"q" name:"quiet" help:"Suppress non-essential output (diagram and step instructions)"`

	// Internal: injected harness for testing.
	testHarness harness.Harness
}

// WithHarness returns a copy with injected harness for testing.
func (c *StageCmd) WithHarness(h harness.Harness) *StageCmd {
	c.testHarness = h
	return c
}

// Run executes the stage command.
func (c *StageCmd) Run() error {
	if _, err := preflightProject(); err != nil {
		return err
	}
	if err := migrate.EnsureSchema(c.Task); err != nil {
		return err
	}

	// Routine tasks take a different path: gate-option resolution and
	// step-id matching live in pkg/routine. Branch early so the rest of
	// the function (workflow flavor) stays unchanged.
	t, err := task.Load(c.Task)
	if err != nil {
		return err
	}
	if t.Routine != "" {
		return c.runRoutineStage(t)
	}

	return fmt.Errorf("task %q has no routine\n\nsubtask stage only works for routine-based tasks (drafted with --routine)", c.Task)
}

// runRoutineStage handles `subtask stage` for routine-driven tasks.
//
// Resolution order for the positional <stage> arg:
//  1. If the current step is `kind: gate`, match arg against option
//     names first. Match → advance to that option's `to:` step.
//  2. Otherwise, match arg against step ids in the routine.
//  3. Else error, listing both option names AND option `to:` targets
//     when applicable.
func (c *StageCmd) runRoutineStage(t *task.Task) error {
	r, err := routine.LoadByName(t.Routine)
	if err != nil {
		return err
	}

	tail, _ := history.Tail(c.Task)
	currentID := tail.Stage
	if currentID == "" {
		currentID = r.EntryStep()
	}
	current := r.GetStep(currentID)

	// Resolve the requested name → target step id.
	target, err := resolveRoutineStageArg(r, current, c.Stage)
	if err != nil {
		return err
	}

	// Guard: fail if a worker is currently running.
	if err := task.WithLock(c.Task, func() error {
		state, _ := task.LoadState(c.Task)
		if state != nil && state.SupervisorPID != 0 && !state.IsStale() {
			return fmt.Errorf("task %q is working\n\nWait for it to finish first", c.Task)
		}
		return nil
	}); err != nil {
		return err
	}

	targetStep := r.GetStep(target)
	if targetStep == nil {
		return fmt.Errorf("routine %q: target step %q not found", r.Name, target)
	}

	// Resolve agent for the target step (agent-swap + session-clear
	// semantics are identical to workflow stage transitions; share the
	// same helper). The from-step is resolved by closure inside
	// ApplyStageTransition's lock so a concurrent routine auto-advance
	// (from a worker reply that just landed) can't observe the same
	// stale fromStage.
	toAgentSpec, toAgentName, err := routine.ResolveStepAgent(targetStep)
	if err != nil {
		return err
	}
	resolveFrom := func(raw string) workspace.FromState {
		if raw == "" {
			raw = r.EntryStep()
		}
		agentName := ""
		if s := r.GetStep(raw); s != nil {
			agentName = routine.StepAgentName(s)
		}
		return workspace.FromState{Stage: raw, AgentName: agentName}
	}
	_ = current // resolveFrom re-derives from history.Tail; outside-lock `current` is used only for the gate-arg resolution above.

	ts := time.Now().UTC()
	from, err := workspace.ApplyStageTransition(c.Task, target, toAgentName, toAgentSpec, ts, resolveFrom)
	if err != nil {
		return err
	}
	// No-op: the task was already on this step (checked under lock, so the
	// observation is the actual current step, not a stale pre-lock read).
	if from.NoOp {
		fmt.Printf("already on step %s\n", target)
		return nil
	}
	// Use the lock-observed from for the display header below.
	currentID = from.Stage

	// Note: routine.surfaced is intentionally NOT emitted from the
	// manual-advance path. The runner emits it on auto-advance so the
	// lead sees the handoff; when the lead is the one driving the
	// transition (this code path), they've already engaged with the
	// task and surfacing it as unread would be confusing.

	// Print result.
	header := fmt.Sprintf("%s: %s", c.Task, target)
	if currentID != "" && currentID != target {
		header = fmt.Sprintf("%s: %s → %s", c.Task, currentID, target)
	}
	if suffix := routine.SourceSuffix(r.Source); suffix != "" {
		header += suffix
	}
	if targetStep.Agent != "" {
		header += fmt.Sprintf(" | agent: %s", targetStep.Agent)
	}
	printSuccess(header)

	// Auto-dispatch when the new step is agent- or worker_instructions-driven,
	// unless --no-send. Gate and terminal steps never auto-dispatch.
	workerInstructions := strings.TrimSpace(targetStep.WorkerInstructions)
	hasAgent := targetStep.Agent != ""
	extraPrompt := strings.TrimSpace(c.Prompt)
	regular := targetStep.Kind == ""
	shouldDispatch := !c.NoSend && regular && (hasAgent || workerInstructions != "" || extraPrompt != "")

	if shouldDispatch {
		leadPrompt := extraPrompt
		dispatchSource := "prompt"
		switch {
		case (hasAgent || workerInstructions != "") && extraPrompt != "":
			dispatchSource = "step + prompt"
		case workerInstructions != "" || hasAgent:
			leadPrompt = fmt.Sprintf("Proceed with the %s step.", targetStep.ID)
			dispatchSource = "step"
		}
		preview := []rune(leadPrompt)
		if len(preview) > 60 {
			preview = append(preview[:60], []rune("...")...)
		}
		fmt.Printf("\nAgent dispatched (%s): %q\n", dispatchSource, string(preview))
		return (&SendCmd{Task: c.Task, Prompt: leadPrompt, Quiet: c.Quiet, testHarness: c.testHarness}).Run()
	}

	// Passive path: print lead-facing step guidance (suppressed by -q).
	if !c.Quiet {
		if v, _ := store.BuildView(context.Background(), c.Task, nil, store.BuildViewOptions{Stage: target}); v != nil && v.Routine != nil && v.Routine.Instructions != "" {
			fmt.Println()
			fmt.Printf("Step: %s\n", render.FormatRoutineDiagram(routineDiagramSteps(v.Routine.Steps), target))
			fmt.Println()
			displayName := target
			if len(displayName) > 0 {
				displayName = strings.ToUpper(displayName[:1]) + displayName[1:]
			}
			fmt.Printf("%s:\n", displayName)
			lines := strings.Split(strings.TrimSpace(v.Routine.Instructions), "\n")
			for _, line := range lines {
				line = strings.ReplaceAll(line, "<task>", c.Task)
				fmt.Printf("  %s\n", line)
			}
		}
	}

	return nil
}

// resolveRoutineStageArg resolves the user's `subtask stage <task> <arg>`
// positional. See runRoutineStage for the order.
func resolveRoutineStageArg(r *routine.Routine, current *routine.Step, arg string) (string, error) {
	if current != nil && current.Kind == routine.KindGate {
		for _, opt := range current.Options {
			if opt.Name == arg {
				return opt.Next, nil
			}
		}
	}

	if s := r.GetStep(arg); s != nil {
		return s.ID, nil
	}

	// Error message lists both option names and reachable destinations
	// when on a gate, plus all step ids — gives the lead a complete
	// recovery surface in one error message.
	var optNames, optTargets []string
	if current != nil && current.Kind == routine.KindGate {
		for _, opt := range current.Options {
			optNames = append(optNames, opt.Name)
			optTargets = append(optTargets, opt.Next)
		}
	}
	stepIDs := r.StepIDs()

	if len(optNames) > 0 {
		return "", fmt.Errorf("unknown stage %q\n\nGate options: %s\nGate targets: %s\nAll step ids: %s",
			arg, strings.Join(optNames, ", "), strings.Join(optTargets, ", "), strings.Join(stepIDs, ", "))
	}
	return "", fmt.Errorf("unknown step %q\n\nValid step ids: %s", arg, strings.Join(stepIDs, ", "))
}

