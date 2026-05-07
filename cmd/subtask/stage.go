package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/render"
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

	// Load workflow from task folder
	wf, err := workflow.LoadFromTask(c.Task)
	if err != nil {
		return fmt.Errorf("failed to load workflow: %w", err)
	}
	if wf == nil {
		return fmt.Errorf("task %q has no workflow\n\nStage is only for tasks created with --workflow", c.Task)
	}

	// Validate stage exists
	if wf.StageIndex(c.Stage) < 0 {
		return fmt.Errorf("unknown stage %q\n\nValid stages: %s", c.Stage, strings.Join(wf.StageNames(), ", "))
	}

	var oldStage string
	var fromPreset, toPreset string
	if err := task.WithLock(c.Task, func() error {
		state, _ := task.LoadState(c.Task)
		if state != nil && state.SupervisorPID != 0 && !state.IsStale() {
			return fmt.Errorf("task %q is working\n\nWait for it to finish first", c.Task)
		}

		tail, _ := history.Tail(c.Task)
		oldStage = tail.Stage
		if oldStage == "" {
			oldStage = wf.FirstStage()
		}

		// Harness swap: if the new stage has a preset binding in the workflow,
		// resolve it and update the task's locked harness fields. Clear the
		// session if the adapter changes — a fresh session on the new adapter
		// reads the workspace, PLAN.md, and PROGRESS.json for cross-stage
		// context (file-based collaboration; see design principle #5).
		newStage := wf.GetStage(c.Stage)
		if oldS := wf.GetStage(oldStage); oldS != nil {
			fromPreset = oldS.Preset
		}
		if newStage != nil && newStage.Preset != "" {
			p, ok := cfg.Presets[newStage.Preset]
			if !ok {
				return fmt.Errorf("workflow stage %q references unknown preset %q\n\nAvailable: %s",
					c.Stage, newStage.Preset, workspace.PresetNames(cfg))
			}
			toPreset = newStage.Preset

			t, err := task.Load(c.Task)
			if err != nil {
				return err
			}
			oldAdapter := t.Adapter
			if p.Adapter != "" {
				t.Adapter = p.Adapter
			}
			if p.Provider != "" {
				t.Provider = p.Provider
			}
			if p.Model != "" {
				t.Model = p.Model
			}
			if p.Reasoning != "" {
				t.Reasoning = p.Reasoning
			}
			if err := t.Save(); err != nil {
				return fmt.Errorf("failed to save task after harness swap: %w", err)
			}

			// Clear the session if the adapter actually changed.
			if state != nil && p.Adapter != "" && p.Adapter != oldAdapter {
				state.SessionID = ""
				state.Adapter = p.Adapter
				if err := state.Save(c.Task); err != nil {
					return fmt.Errorf("failed to clear session after adapter swap: %w", err)
				}
			}
		}

		data, _ := json.Marshal(map[string]any{
			"from":        oldStage,
			"to":          c.Stage,
			"from_preset": fromPreset,
			"to_preset":   toPreset,
		})
		return history.AppendLocked(c.Task, history.Event{TS: time.Now().UTC(), Type: "stage.changed", Data: data})
	}); err != nil {
		return err
	}

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
