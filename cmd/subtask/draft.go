package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/agent"
	"github.com/kgruel/subtask/pkg/git"
	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/migrate/gitredesign"
	"github.com/kgruel/subtask/pkg/task/store"
	"github.com/kgruel/subtask/pkg/workspace"
)

// DraftCmd implements 'subtask draft'.
type DraftCmd struct {
	Task        string `arg:"" help:"Task name (e.g., fix/epoch-boundary)"`
	Description string `arg:"" optional:"" help:"Task description (or use stdin)"`
	Base        string `name:"base-branch" help:"Base branch (defaults to the current branch)"`
	Title       string `required:"" help:"Short description"`
	Adapter     string `help:"Adapter for this task (overrides project config)"`
	Provider    string `help:"Provider for this task (adapter-dependent; overrides project config)"`
	Model       string `help:"Default model for this task (overrides project config)"`
	Reasoning   string `help:"Default reasoning for this task (adapter-dependent; overrides project config)"`
	FollowUp    string `name:"follow-up" help:"Task whose conversation to continue"`
	Agent       string `help:"Agent file from .subtask/agents/<name>.yaml; bundles dispatch + role prompt. Mutually exclusive with --routine"`
	Routine     string `help:"Routine file from .subtask/routines/<name>.yaml; runs a multi-step recipe. Mutually exclusive with --agent"`
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
	if _, err := preflightProject(); err != nil {
		return err
	}

	// Check if task already exists
	if _, err := task.Load(c.Task); err == nil {
		return fmt.Errorf("task %q already exists", c.Task)
	}

	if strings.Contains(c.Task, "--") {
		return fmt.Errorf("task name cannot contain \"--\" (used for path escaping)")
	}

	// --routine and --agent are mutually exclusive. Routine steps define
	// their own per-step agents; harness.BuildPrompt sources the ## Agent
	// block from the current routine step (not t.Agent) for routine
	// tasks. Persisting t.Agent alongside t.Routine would create mixed
	// state: the worker would run with the agent's preset (adapter/model)
	// while reading the routine step's role prompt — silently
	// inconsistent.
	if c.Routine != "" && c.Agent != "" {
		return fmt.Errorf("--agent and --routine are mutually exclusive: routine steps define their own agents.\n\nUse the routine's step config to set per-step agents")
	}

	if c.Base == "" {
		branch, err := git.CurrentBranch(task.ProjectRoot())
		if err != nil {
			return fmt.Errorf("could not determine current branch (pass --base-branch explicitly): %w", err)
		}
		branch = strings.TrimSpace(branch)
		if branch == "" || branch == "HEAD" {
			return fmt.Errorf("not on a branch (detached HEAD?); pass --base-branch explicitly")
		}
		c.Base = branch
	}

	if c.Routine != "" {
		return c.runRoutineDraft(description)
	}

	// Resolve adapter/model/reasoning with this precedence:
	//   1. --agent's dispatch fields (overlay via ApplyAgentSpec).
	//   2. Anything still unset falls through to project then user config defaults.
	resolvedAdapter := c.Adapter
	resolvedProvider := c.Provider
	resolvedModel := c.Model
	resolvedReasoning := c.Reasoning

	if c.Agent != "" {
		ag, err := agent.LoadByName(c.Agent)
		if err != nil {
			return err
		}
		spec := ag.AgentSpec()
		workspace.ApplyAgentSpec(spec, &resolvedAdapter, &resolvedProvider, &resolvedModel, &resolvedReasoning)
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
		Adapter:     resolvedAdapter,
		Provider:    resolvedProvider,
		Model:       resolvedModel,
		Reasoning:   resolvedReasoning,
		Agent:       c.Agent,
		Schema:      gitredesign.TaskSchemaVersion,
	}

	if err := t.Save(); err != nil {
		return fmt.Errorf("failed to save task: %w", err)
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
		"title":       c.Title,
		"follow_up":   c.FollowUp,
		"adapter":     resolvedAdapter,
		"model":       resolvedModel,
		"reasoning":   resolvedReasoning,
		"base_ref":    baseRef,
		"base_commit": baseCommit,
	})
	if err := history.WriteAll(c.Task, []history.Event{
		{TS: time.Now().UTC(), Type: "task.opened", Data: openedData},
	}); err != nil {
		return fmt.Errorf("failed to write history: %w", err)
	}

	if c.FollowUp != "" {
		childData, _ := json.Marshal(map[string]any{
			"child_name":  c.Task,
			"base_commit": baseCommit,
		})
		if err := history.Append(c.FollowUp, history.Event{
			TS:   time.Now().UTC(),
			Type: "child.drafted",
			Data: childData,
		}); err != nil {
			return fmt.Errorf("failed to write child.drafted to parent history: %w", err)
		}
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

	if c.Agent != "" {
		summary := resolvedAdapter + "/" + resolvedModel
		if resolvedReasoning != "" {
			summary += " (" + resolvedReasoning + " reasoning)"
		}
		printSection("Agent: " + c.Agent)
		fmt.Println(summary)
		fmt.Println()
	}

	printSection("Usage")
	fmt.Printf("subtask send %s \"<prompt>\"\n", c.Task)
	fmt.Printf("subtask next %s\n", c.Task)

	return nil
}

// runRoutineDraft drafts a routine-driven task. Diverges from the
// workflow path in two important ways:
//   - No <task>/WORKFLOW.yaml is copied (routines resolve by reference;
//     see audit Q3).
//   - The initial stage is the routine's entry step id (not the
//     workflow's FirstStage). Preset/agent from the entry step is
//     applied to the adapter/model snapshot.
//
// Reuses the same task.opened + stage.changed history events so
// downstream tooling (history.Tail, list, TUI) does not have to learn a
// new event shape.
func (c *DraftCmd) runRoutineDraft(description string) error {
	r, err := routine.LoadByName(c.Routine)
	if err != nil {
		return err
	}
	if len(r.Steps) == 0 {
		// LoadByName/validateSteps catches this; double-checked here so
		// the panic from r.Steps[0] is impossible.
		return fmt.Errorf("routine %q has no steps", c.Routine)
	}
	// Fail-fast: validate every step's agent/preset against the live
	// environment so a typo in a later step doesn't pass draft and
	// surface mid-routine after worker rounds have already run.
	// Mirrors the workflow draft path, which validates all stage
	// presets up front.
	if err := r.ValidateReferences(); err != nil {
		return err
	}

	resolvedAdapter := c.Adapter
	resolvedProvider := c.Provider
	resolvedModel := c.Model
	resolvedReasoning := c.Reasoning

	// Note: c.Agent is rejected with c.Routine at the top of Run(); no
	// agent overlay applies here. Routine tasks pick agents per step,
	// not from a draft-time flag.

	// Entry step agent fills any remaining adapter/model fields.
	entry := &r.Steps[0]
	if entry.Agent != "" {
		ag, err := agent.LoadByName(entry.Agent)
		if err != nil {
			return fmt.Errorf("routine %q entry step %q: %w", c.Routine, entry.ID, err)
		}
		spec := ag.AgentSpec()
		workspace.ApplyAgentSpec(spec, &resolvedAdapter, &resolvedProvider, &resolvedModel, &resolvedReasoning)
	}

	if err := workspace.ValidateReasoningLevel(resolvedReasoning); err != nil {
		return err
	}

	// t.Agent is intentionally left empty for routine tasks. The
	// per-step agent lives in the routine YAML; persisting c.Agent
	// would be dead state — BuildPrompt sources the ## Agent block
	// from the current routine step, not from t.Agent, when
	// t.Routine != "".
	t := &task.Task{
		Name:        c.Task,
		Title:       c.Title,
		BaseBranch:  c.Base,
		Description: description,
		FollowUp:    c.FollowUp,
		Adapter:     resolvedAdapter,
		Provider:    resolvedProvider,
		Model:       resolvedModel,
		Reasoning:   resolvedReasoning,
		Routine:     c.Routine,
		Schema:      gitredesign.TaskSchemaVersion,
	}
	if err := t.Save(); err != nil {
		return fmt.Errorf("failed to save task: %w", err)
	}

	repoRoot := task.ProjectRoot()
	baseRef := c.Base
	baseCommit, err := git.Output(repoRoot, "rev-parse", baseRef)
	if err != nil {
		return fmt.Errorf("failed to resolve base branch %q: %w", baseRef, err)
	}

	openedData, _ := json.Marshal(map[string]any{
		"reason":      "draft",
		"branch":      c.Task,
		"base_branch": c.Base,
		"routine":     r.Name,
		"title":       c.Title,
		"follow_up":   c.FollowUp,
		"adapter":     resolvedAdapter,
		"model":       resolvedModel,
		"reasoning":   resolvedReasoning,
		"base_ref":    baseRef,
		"base_commit": baseCommit,
	})
	stageData, _ := json.Marshal(map[string]any{
		"from": "",
		"to":   entry.ID,
	})
	if err := history.WriteAll(c.Task, []history.Event{
		{TS: time.Now().UTC(), Type: "task.opened", Data: openedData},
		{TS: time.Now().UTC(), Type: "stage.changed", Data: stageData},
	}); err != nil {
		return fmt.Errorf("failed to write history: %w", err)
	}

	if c.FollowUp != "" {
		childData, _ := json.Marshal(map[string]any{
			"child_name":  c.Task,
			"base_commit": baseCommit,
		})
		if err := history.Append(c.FollowUp, history.Event{
			TS:   time.Now().UTC(),
			Type: "child.drafted",
			Data: childData,
		}); err != nil {
			return fmt.Errorf("failed to write child.drafted to parent history: %w", err)
		}
	}

	printSuccess(fmt.Sprintf("Drafted task: %s", c.Task))
	fmt.Printf("Task folder: %s/\n", filepath.ToSlash(task.Dir(c.Task)))
	fmt.Println("  Files here are shared with worker (PLAN.md, notes, etc.)")
	fmt.Println()

	if pending := unreadTaskNames(c.Task); len(pending) >= 2 {
		fmt.Fprintf(os.Stderr, "Note: %d task(s) already awaiting your review (%s).\n",
			len(pending), strings.Join(pending, ", "))
		fmt.Fprintln(os.Stderr, "Parallel workers ≤ your review bandwidth — consider reviewing before queueing more.")
		fmt.Fprintln(os.Stderr)
	}

	v, _ := store.BuildView(context.Background(), c.Task, nil, store.BuildViewOptions{})
	if v != nil && v.Routine != nil {
		printSection("Routine: " + v.Routine.Name + routine.SourceSuffix(v.Routine.Source))
		fmt.Println(v.Routine.Diagram)
		if v.Routine.StepAgent != "" {
			fmt.Printf("Agent: %s\n", v.Routine.StepAgent)
		}
		fmt.Println()

		if v.Routine.Instructions != "" {
			lines := strings.Split(strings.TrimSpace(v.Routine.Instructions), "\n")
			for _, line := range lines {
				line = strings.ReplaceAll(line, "<task>", c.Task)
				fmt.Println(line)
			}
			fmt.Println()
		}
	}

	printSection("Usage")
	fmt.Printf("subtask send %s \"<prompt>\"\n", c.Task)
	fmt.Printf("subtask next %s\n", c.Task)

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
