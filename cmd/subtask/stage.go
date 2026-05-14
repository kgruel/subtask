package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/render"
	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/migrate"
	"github.com/kgruel/subtask/pkg/workflow"
	"github.com/kgruel/subtask/pkg/workspace"
)

// StageCmd implements 'subtask stage'.
type StageCmd struct {
	Task   string `arg:"" help:"Task name"`
	Stage  string `arg:"" help:"Stage to set"`
	Prompt string `arg:"" optional:"" help:"Extra user message sent alongside the new stage's worker_instructions (or alone if there are none)"`
	NoSend bool   `name:"no-send" help:"Skip auto-dispatch even if the new stage has worker_instructions"`

	// Internal: injected harness for testing.
	testHarness harness.Harness
}

// WithHarness returns a copy with injected harness for testing.
func (c *StageCmd) WithHarness(h harness.Harness) *StageCmd {
	c.testHarness = h
	return c
}

// stageTransitionResult holds the resolved values from a stage transition.
type stageTransitionResult struct {
	FromStage string
	ToStage   string
	ToPreset  string
}

// transitionStage validates the target stage's preset, updates TASK.md and
// state if the adapter changes, and appends a stage.changed event. It is
// safe to call from both StageCmd and the send.go auto-advance path.
//
// Workflow-specific wrapper: resolves the to-stage preset by name and
// supplies a from-stage resolver to workspace.ApplyStageTransition,
// which reads the raw fromStage from history.Tail INSIDE the lock so
// concurrent transitions can't both observe a stale fromStage.
func transitionStage(taskName, toStage string, cfg *workspace.Config, wf *workflow.Workflow, ts time.Time) (stageTransitionResult, error) {
	var result stageTransitionResult
	result.ToStage = toStage

	var toPreset *workspace.Preset
	toPresetName := ""
	if newStage := wf.GetStage(toStage); newStage != nil && newStage.Preset != "" {
		p, ok := cfg.Presets[newStage.Preset]
		if !ok {
			return result, fmt.Errorf("workflow stage %q references unknown preset %q\n\nAvailable: %s",
				toStage, newStage.Preset, workspace.PresetNames(cfg))
		}
		toPreset = &p
		toPresetName = newStage.Preset
		result.ToPreset = newStage.Preset
	}

	resolveFrom := func(raw string) workspace.FromState {
		if raw == "" {
			raw = wf.FirstStage()
		}
		preset := ""
		if s := wf.GetStage(raw); s != nil {
			preset = s.Preset
		}
		return workspace.FromState{Stage: raw, PresetName: preset}
	}

	from, err := workspace.ApplyStageTransition(taskName, toStage, toPresetName, toPreset, ts, resolveFrom)
	if err != nil {
		return result, err
	}
	result.FromStage = from.Stage
	return result, nil
}

// Run executes the stage command.
func (c *StageCmd) Run() error {
	res, err := preflightProject()
	if err != nil {
		return err
	}
	cfg := res.Config

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
		return c.runRoutineStage(t, cfg)
	}

	// Load workflow from task folder
	wf, err := workflow.LoadFromTask(c.Task)
	if err != nil {
		return fmt.Errorf("failed to load workflow: %w", err)
	}
	if wf == nil {
		return fmt.Errorf("task %q has no workflow\n\nStage is only for tasks created with --workflow or --routine", c.Task)
	}

	// Validate stage exists
	if wf.StageIndex(c.Stage) < 0 {
		return fmt.Errorf("unknown stage %q\n\nValid stages: %s", c.Stage, strings.Join(wf.StageNames(), ", "))
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

	tr, err := transitionStage(c.Task, c.Stage, cfg, wf, time.Now().UTC())
	if err != nil {
		return err
	}
	oldStage := tr.FromStage
	toPreset := tr.ToPreset

	// Print result
	header := fmt.Sprintf("%s: %s", c.Task, c.Stage)
	if oldStage != "" && oldStage != c.Stage {
		header = fmt.Sprintf("%s: %s → %s", c.Task, oldStage, c.Stage)
	}
	if toPreset != "" {
		t, _ := task.Load(c.Task)
		header += fmt.Sprintf(" | preset: %s", toPreset)
		if t != nil && t.Adapter != "" {
			header += fmt.Sprintf(" (%s)", formatPreset(workspace.Preset{
				Adapter: t.Adapter, Model: t.Model, Reasoning: t.Reasoning,
			}))
		}
	}
	printSuccess(header)

	// Determine whether to auto-dispatch.
	//
	// BuildPrompt (pkg/harness/prompt.go) already injects the new stage's
	// worker_instructions into a "## Stage:" block on every send, so we only
	// pass the lead's positional prompt as the user message. Including
	// worker_instructions here would produce them twice.
	newStageObj := wf.GetStage(c.Stage)
	workerInstructions := ""
	if newStageObj != nil {
		workerInstructions = strings.TrimSpace(newStageObj.WorkerInstructions)
	}
	extraPrompt := strings.TrimSpace(c.Prompt)
	shouldDispatch := !c.NoSend && (workerInstructions != "" || extraPrompt != "")

	if shouldDispatch {
		leadPrompt := extraPrompt
		dispatchSource := "prompt"
		switch {
		case workerInstructions != "" && extraPrompt != "":
			dispatchSource = "worker_instructions + prompt"
		case workerInstructions != "":
			// SendCmd requires a non-empty prompt; the worker_instructions are
			// in the "## Stage:" block, so we just need a short trigger.
			leadPrompt = fmt.Sprintf("Proceed with the %s stage.", c.Stage)
			dispatchSource = "worker_instructions"
		}
		preview := []rune(leadPrompt)
		if len(preview) > 60 {
			preview = append(preview[:60], []rune("...")...)
		}
		fmt.Printf("\nWorker dispatched (%s): %q\n", dispatchSource, string(preview))
		return (&SendCmd{Task: c.Task, Prompt: leadPrompt, testHarness: c.testHarness}).Run()
	}

	// Passive path: print lead-facing stage guidance.
	if newStageObj != nil && newStageObj.Instructions != "" {
		fmt.Println()
		printStageGuidance(c.Task, wf, c.Stage)
	}

	return nil
}

// runRoutineStage handles `subtask stage` for routine-driven tasks.
//
// Resolution order for the positional <stage> arg:
//  1. If the current step is `kind: gate`, match arg against option
//     names first. Match → advance to that option's `to:` step.
//  2. Otherwise, match arg against step ids in the routine.
//  3. Else error, listing both option names AND option `to:` targets
//     when applicable.
func (c *StageCmd) runRoutineStage(t *task.Task, cfg *workspace.Config) error {
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

	// Resolve preset for the target step (preset-swap + session-clear
	// semantics are identical to workflow stage transitions; share the
	// same helper). The from-step is resolved by closure inside
	// ApplyStageTransition's lock so a concurrent routine auto-advance
	// (from a worker reply that just landed) can't observe the same
	// stale fromStage.
	toPreset, toPresetName, err := routine.ResolveStepPreset(targetStep, cfg)
	if err != nil {
		return err
	}
	resolveFrom := func(raw string) workspace.FromState {
		if raw == "" {
			raw = r.EntryStep()
		}
		preset := ""
		if s := r.GetStep(raw); s != nil {
			preset = routine.StepPresetName(s)
		}
		return workspace.FromState{Stage: raw, PresetName: preset}
	}
	_ = current // resolveFrom re-derives from history.Tail; outside-lock `current` is used only for the gate-arg resolution above.

	ts := time.Now().UTC()
	from, err := workspace.ApplyStageTransition(c.Task, target, toPresetName, toPreset, ts, resolveFrom)
	if err != nil {
		return err
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
	if toPresetName != "" {
		header += fmt.Sprintf(" | preset: %s", toPresetName)
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
		fmt.Printf("\nWorker dispatched (%s): %q\n", dispatchSource, string(preview))
		return (&SendCmd{Task: c.Task, Prompt: leadPrompt, testHarness: c.testHarness}).Run()
	}

	return nil
}

// resolveRoutineStageArg resolves the user's `subtask stage <task> <arg>`
// positional. See runRoutineStage for the order.
func resolveRoutineStageArg(r *routine.Routine, current *routine.Step, arg string) (string, error) {
	if current != nil && current.Kind == routine.KindGate {
		for _, opt := range current.Options {
			if opt.Name == arg {
				return opt.To, nil
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
			optTargets = append(optTargets, opt.To)
		}
	}
	stepIDs := r.StepIDs()

	if len(optNames) > 0 {
		return "", fmt.Errorf("unknown stage %q\n\nGate options: %s\nGate targets: %s\nAll step ids: %s",
			arg, strings.Join(optNames, ", "), strings.Join(optTargets, ", "), strings.Join(stepIDs, ", "))
	}
	return "", fmt.Errorf("unknown step %q\n\nValid step ids: %s", arg, strings.Join(stepIDs, ", "))
}

// printStageGuidance prints the guidance for a stage.
func printStageGuidance(taskName string, wf *workflow.Workflow, stageName string) {
	stage := wf.GetStage(stageName)
	if stage == nil {
		return
	}

	// Print stage progression
	fmt.Printf("Stage: %s\n", render.FormatStageProgression(wf.StageNames(), stageName))
	fmt.Println()

	// Print stage name and guidance (capitalize first letter)
	displayName := stageName
	if len(displayName) > 0 {
		displayName = strings.ToUpper(displayName[:1]) + displayName[1:]
	}
	fmt.Printf("%s:\n", displayName)
	// Indent guidance
	lines := strings.Split(strings.TrimSpace(stage.Instructions), "\n")
	for _, line := range lines {
		// Replace <task> placeholder with actual task name
		line = strings.ReplaceAll(line, "<task>", taskName)
		fmt.Printf("  %s\n", line)
	}
}
