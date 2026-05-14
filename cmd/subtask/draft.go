package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/git"
	"github.com/kgruel/subtask/pkg/render"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/migrate/gitredesign"
	"github.com/kgruel/subtask/pkg/workflow"
	"github.com/kgruel/subtask/pkg/workspace"
)

// DraftCmd implements 'subtask draft'.
type DraftCmd struct {
	Task        string `arg:"" help:"Task name (e.g., fix/epoch-boundary)"`
	Description string `arg:"" optional:"" help:"Task description (or use stdin)"`
	Base        string `name:"base-branch" required:"" help:"Base branch"`
	Title       string `required:"" help:"Short description"`
	Adapter     string `help:"Adapter for this task (overrides project config)"`
	Provider    string `help:"Provider for this task (adapter-dependent; overrides project config)"`
	Model       string `help:"Default model for this task (overrides project config)"`
	Reasoning   string `help:"Default reasoning for this task (adapter-dependent; overrides project config)"`
	Workflow    string `help:"Workflow template to use (e.g., they-plan)"`
	FollowUp    string `name:"follow-up" help:"Task whose conversation to continue"`
	Type        string `help:"Task type from project config (e.g., implement, review)"`
	Preset      string `help:"Preset from project config (e.g., sonnet-medium); shorthand for --adapter --model --reasoning"`
}

// Run executes the draft command.
func (c *DraftCmd) Run() error {
	// Read description from stdin if not provided as arg
	description := c.Description
	if description == "" {
		description = readStdinForDraft()
	}

	if description == "" {
		return fmt.Errorf("description is required\n\n" +
			"Provide description as argument or via stdin (heredoc/pipe)")
	}

	// Requirements: git + global config (config may be migrated on first access).
	res, err := preflightProject()
	if err != nil {
		return err
	}
	cfg := res.Config

	// Check if task already exists
	if _, err := task.Load(c.Task); err == nil {
		return fmt.Errorf("task %q already exists", c.Task)
	}

	if strings.Contains(c.Task, "--") {
		return fmt.Errorf("task name cannot contain \"--\" (used for path escaping)")
	}

	// Resolve adapter/model/reasoning/workflow with this precedence (each layer
	// only fills fields not already set by an earlier layer):
	//   1. Explicit flags (--adapter/--model/--reasoning/--workflow) win.
	//   2. --preset resolves the named preset.
	//   3. --type resolves the type's default_workflow / default_preset.
	//   4. Follow-up inherits the parent's type.
	resolvedAdapter := c.Adapter
	resolvedProvider := c.Provider
	resolvedModel := c.Model
	resolvedReasoning := c.Reasoning
	resolvedWorkflow := c.Workflow
	resolvedType := c.Type

	// Follow-up inherits the parent's type when caller didn't pick one.
	// Done first so subsequent type-default resolution applies.
	if resolvedType == "" && c.FollowUp != "" {
		if parent, err := task.Load(c.FollowUp); err == nil && parent.Type != "" {
			resolvedType = parent.Type
		}
	}

	if c.Preset != "" {
		p, ok := cfg.Presets[c.Preset]
		if !ok {
			return fmt.Errorf("unknown preset %q\n\nAvailable: %s", c.Preset, workspace.PresetNames(cfg))
		}
		workspace.ApplyPreset(p, &resolvedAdapter, &resolvedProvider, &resolvedModel, &resolvedReasoning)
	}

	if resolvedType != "" {
		tt, ok := cfg.Types[resolvedType]
		if !ok {
			return fmt.Errorf("unknown type %q\n\nAvailable: %s", resolvedType, typeNames(cfg))
		}
		if resolvedWorkflow == "" && tt.DefaultWorkflow != "" {
			resolvedWorkflow = tt.DefaultWorkflow
		}
		if tt.DefaultPreset != "" {
			p, ok := cfg.Presets[tt.DefaultPreset]
			if !ok {
				return fmt.Errorf("type %q references unknown default_preset %q", resolvedType, tt.DefaultPreset)
			}
			workspace.ApplyPreset(p, &resolvedAdapter, &resolvedProvider, &resolvedModel, &resolvedReasoning)
		}
	}

	// Load workflow (default if not specified)
	if resolvedWorkflow == "" {
		resolvedWorkflow = "default"
	}
	wf, err := workflow.Load(workflow.TemplateDir(resolvedWorkflow))
	if err != nil {
		return fmt.Errorf("workflow %q: %w", resolvedWorkflow, err)
	}

	// Validate any preset references in the workflow (workflow.Load doesn't
	// have access to cfg). If the first stage has a preset binding, use it as
	// the starting harness when not already set by an explicit flag/preset/type.
	for _, st := range wf.Stages {
		if st.Preset == "" {
			continue
		}
		if _, ok := cfg.Presets[st.Preset]; !ok {
			return fmt.Errorf("workflow %q stage %q references unknown preset %q\n\nAvailable: %s",
				resolvedWorkflow, st.Name, st.Preset, workspace.PresetNames(cfg))
		}
	}
	if first := wf.GetStage(wf.FirstStage()); first != nil && first.Preset != "" {
		workspace.ApplyPreset(cfg.Presets[first.Preset], &resolvedAdapter, &resolvedProvider, &resolvedModel, &resolvedReasoning)
	}

	// Create task
	if err := workspace.ValidateReasoningLevel(resolvedReasoning); err != nil {
		return err
	}
	t := &task.Task{
		Name:        c.Task,
		Title:       c.Title,
		BaseBranch:  c.Base,
		Description: description,
		FollowUp:    c.FollowUp,
		Type:        resolvedType,
		Adapter:     resolvedAdapter,
		Provider:    resolvedProvider,
		Model:       resolvedModel,
		Reasoning:   resolvedReasoning,
		Schema:      gitredesign.TaskSchemaVersion,
	}

	if err := t.Save(); err != nil {
		return fmt.Errorf("failed to save task: %w", err)
	}

	// Copy workflow files to task folder
	if err := workflow.CopyToTask(resolvedWorkflow, c.Task); err != nil {
		return fmt.Errorf("failed to copy workflow: %w", err)
	}

	// Capture base branch commit for staleness/conflict heuristics.
	repoRoot := task.ProjectRoot()

	// Local-first: capture from the local base branch only. If users want fresh remote
	// state they can run `git fetch` themselves before drafting.
	baseRef := c.Base

	baseCommit, err := git.Output(repoRoot, "rev-parse", baseRef)
	if err != nil {
		return fmt.Errorf("failed to resolve base branch %q: %w", baseRef, err)
	}

	openedData, _ := json.Marshal(map[string]any{
		"reason":      "draft",
		"branch":      c.Task,
		"base_branch": c.Base,
		"workflow":    wf.Name,
		"title":       c.Title,
		"follow_up":   c.FollowUp,
		"type":        resolvedType,
		"adapter":     resolvedAdapter,
		"model":       resolvedModel,
		"reasoning":   resolvedReasoning,
		"base_ref":    baseRef,
		"base_commit": baseCommit,
	})
	stageData, _ := json.Marshal(map[string]any{
		"from": "",
		"to":   wf.FirstStage(),
	})
	if err := history.WriteAll(c.Task, []history.Event{
		{TS: time.Now().UTC(), Type: "task.opened", Data: openedData},
		{TS: time.Now().UTC(), Type: "stage.changed", Data: stageData},
	}); err != nil {
		return fmt.Errorf("failed to write history: %w", err)
	}

	// Output
	printSuccess(fmt.Sprintf("Drafted task: %s", c.Task))
	fmt.Printf("Task folder: %s/\n", filepath.ToSlash(task.Dir(c.Task)))
	fmt.Println("  Files here are shared with worker (PLAN.md, notes, etc.)")
	fmt.Println()

	// Soft parallelism caution: lead review is serial, worker time is parallel.
	// If the lead already has multiple unread worker replies, queueing more
	// drafts costs more than it gains. Visibility, not enforcement.
	if pending := unreadTaskNames(c.Task); len(pending) >= 2 {
		fmt.Fprintf(os.Stderr, "Note: %d task(s) already awaiting your review (%s).\n",
			len(pending), strings.Join(pending, ", "))
		fmt.Fprintln(os.Stderr, "Parallel workers ≤ your review bandwidth — consider reviewing before queueing more.")
		fmt.Fprintln(os.Stderr)
	}

	// Show lead instructions from workflow
	if wf.Instructions.Lead != "" {
		printSection("Workflow: " + wf.Name)
		printSectionContent(wf.Instructions.Lead)
	}

	// Show current stage
	printSection("Stage: " + wf.FirstStage())
	fmt.Println(render.FormatStageProgression(wf.StageNames(), wf.FirstStage()))
	fmt.Println()

	stage := wf.GetStage(wf.FirstStage())
	if stage != nil && stage.Instructions != "" {
		lines := strings.Split(strings.TrimSpace(stage.Instructions), "\n")
		for _, line := range lines {
			line = strings.ReplaceAll(line, "<task>", c.Task)
			fmt.Println(line)
		}
	}

	// Show how to run
	printSection("Usage")
	fmt.Printf("subtask send %s \"<prompt>\"\n", c.Task)

	return nil
}

// unreadTaskNames returns names of open tasks whose most recent worker reply
// has not been read by the lead, excluding `exclude` (the task just drafted).
// Errors are swallowed — this is advisory only, never blocks draft.
//
// Iterates index-open tasks (same view `subtask list` uses) rather than disk
// folders, so closed/merged or otherwise cleaned-up tasks don't surface as
// phantom unread entries.
func unreadTaskNames(exclude string) []string {
	names, err := openTaskNames()
	if err != nil {
		return nil
	}
	var pending []string
	for _, name := range names {
		if name == exclude {
			continue
		}
		unread, err := taskHasUnreadReply(name)
		if err != nil {
			continue
		}
		if unread {
			pending = append(pending, name)
		}
	}
	return pending
}

// readStdinForDraft reads from stdin if data is piped/heredoc.
func readStdinForDraft() string {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return ""
	}
	// Only read if stdin is a pipe or has data (not a terminal)
	mode := fi.Mode()
	if (mode&os.ModeCharDevice) != 0 || (mode&os.ModeNamedPipe) == 0 && fi.Size() == 0 {
		return ""
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
