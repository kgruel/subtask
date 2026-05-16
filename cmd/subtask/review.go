package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/agent"
	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/workspace"
)

// reviewStartedData is the JSON payload for a review.started history event.
type reviewStartedData struct {
	RunID        string `json:"run_id"`
	Kind         string `json:"kind"`
	Adapter      string `json:"adapter,omitempty"`
	Model        string `json:"model,omitempty"`
	Reasoning    string `json:"reasoning,omitempty"`
	Instructions string `json:"instructions,omitempty"`
}

// reviewFinishedData is the JSON payload for a review.finished history event.
type reviewFinishedData struct {
	RunID      string `json:"run_id"`
	Kind       string `json:"kind"`
	DurationMS int    `json:"duration_ms"`
	Outcome    string `json:"outcome"`
	File       string `json:"file,omitempty"`
	Error      string `json:"error,omitempty"`
}

// persistReviewFile writes the review text to the task's reviews/ subdirectory.
// The filename encodes timestamp, runID, kind, and adapter for uniqueness and
// sortability: <timestamp>-<runID>-<kind>-<adapter>.md.
// Returns the relative path from the task folder root (e.g. "reviews/...md").
func persistReviewFile(taskName, kind, adapter, runID, review string) (string, error) {
	dir := task.ReviewsDir(taskName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ts := time.Now().UTC().Format("20060102T150405Z")
	name := ts + "-" + runID + "-" + kind + "-" + adapter + ".md"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(review), 0o644); err != nil {
		return "", err
	}
	return "reviews/" + name, nil
}

// ReviewCmd implements 'subtask review'.
type ReviewCmd struct {
	// Target selection (mutually exclusive)
	Task        string `help:"Review changes in a task workspace against that task's base branch"`
	Base        string `help:"Review changes on the current branch against BRANCH (PR-style diff via merge-base; BRANCH must be a valid git ref)"`
	Uncommitted bool   `help:"Review uncommitted changes (staged, unstaged, untracked)"`
	Commit      string `help:"Review changes introduced by a specific commit SHA"`

	// Plan modifies --task to review PLAN.md against the task's spec instead
	// of the diff. Catches plan-vs-spec drift before implementation lands.
	Plan bool `help:"With --task, review PLAN.md against the task spec (TASK.md) instead of the diff"`

	// Optional instructions
	Prompt string `arg:"" optional:"" help:"Additional review instructions (or use stdin)"`

	// Adapter/model/reasoning overrides (do not persist)
	Adapter   string `help:"Override adapter for this review (does not persist)"`
	Agent     string `help:"Agent override for adapter/model/reasoning (does not persist)"`
	Model     string `help:"Override model for this review"`
	Reasoning string `help:"Override reasoning effort (low, medium, high, xhigh)"`

	// Internal: injected harness for testing
	testHarness harness.Harness
}

// WithHarness returns a copy with injected harness for testing.
func (c *ReviewCmd) WithHarness(h harness.Harness) *ReviewCmd {
	c.testHarness = h
	return c
}

// Run executes the review command.
func (c *ReviewCmd) Run() error {
	// Validate mutually exclusive flags
	count := 0
	if strings.TrimSpace(c.Task) != "" {
		count++
	}
	if strings.TrimSpace(c.Base) != "" {
		count++
	}
	if c.Uncommitted {
		count++
	}
	if strings.TrimSpace(c.Commit) != "" {
		count++
	}
	if count > 1 {
		return fmt.Errorf("--task, --base, --uncommitted, and --commit are mutually exclusive")
	}
	if count == 0 {
		return fmt.Errorf("specify one of: --task <name>, --base <branch>, --uncommitted, or --commit <sha>")
	}
	if c.Plan && strings.TrimSpace(c.Task) == "" {
		return fmt.Errorf("--plan requires --task <name>")
	}

	// Read instructions from arg or stdin
	instructions := strings.TrimSpace(c.Prompt)
	if instructions == "" {
		instructions = readStdinIfAvailable()
	}

	// Requirements: git + global config (config may be migrated on first access).
	res, err := preflightProject()
	if err != nil {
		return err
	}
	cfg := res.Config

	// Load task when --task is set: needed for adapter/model resolution, not just base branch.
	var t *task.Task
	if strings.TrimSpace(c.Task) != "" {
		loaded, err := task.Load(strings.TrimSpace(c.Task))
		if err != nil {
			return fmt.Errorf("failed to load task %q: %w", strings.TrimSpace(c.Task), err)
		}
		t = loaded
	}

	// Resolve adapter/model/reasoning.
	var agentOverride *workspace.AgentSpec
	if c.Agent != "" {
		ag, agErr := agent.LoadByName(c.Agent)
		if agErr != nil {
			return agErr
		}
		spec := ag.AgentSpec()
		agentOverride = &spec
	}
	r, err := workspace.Resolve(cfg, t, workspace.ResolveOverrides{
		Adapter:   c.Adapter,
		Model:     c.Model,
		Reasoning: c.Reasoning,
		Agent:     agentOverride,
	})
	if err != nil {
		return err
	}

	// Plan-review path: read PLAN.md and TASK.md, run a fresh prompt that
	// asks the harness to find drift between spec intent and plan steps.
	// Bypasses the diff-review machinery because there's no diff to inspect.
	if c.Plan {
		return c.runPlanReview(cfg, t, r, instructions)
	}

	// Determine working directory and target.
	taskName := strings.TrimSpace(c.Task)
	var cwd string
	var target harness.ReviewTarget

	switch {
	case taskName != "":
		// Load state (for workspace path)
		state, err := task.LoadState(taskName)
		if err != nil {
			return err
		}
		if state == nil || state.Workspace == "" {
			return fmt.Errorf("task %q has no workspace\n\nRun the task first:\n  subtask send %s \"...\"", taskName, taskName)
		}

		cwd = state.Workspace
		target = harness.ReviewTarget{TaskName: taskName, BaseBranch: t.BaseBranch}

	case strings.TrimSpace(c.Base) != "":
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		target = harness.ReviewTarget{BaseBranch: strings.TrimSpace(c.Base)}

	case c.Uncommitted:
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		target = harness.ReviewTarget{Uncommitted: true}

	case strings.TrimSpace(c.Commit) != "":
		cwd, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		target = harness.ReviewTarget{Commit: strings.TrimSpace(c.Commit)}
	}

	// Build harness
	var h harness.Harness
	if c.testHarness != nil {
		h = c.testHarness
	} else {
		h, err = harness.New(workspace.ConfigWithOverrides(cfg, r.Adapter, r.Provider, r.Model, r.Reasoning))
		if err != nil {
			return err
		}
	}

	// Non-task reviews: no event sourcing, straight to stdout.
	if taskName == "" {
		review, err := h.Review(cwd, target, instructions)
		if err != nil {
			return err
		}
		fmt.Println(review)
		return nil
	}

	// Task-scoped diff review: append review.started, run, persist, append review.finished.
	runID, _ := history.NewRunID()

	startData, _ := json.Marshal(reviewStartedData{
		RunID:        runID,
		Kind:         "diff",
		Adapter:      r.Adapter,
		Model:        r.Model,
		Reasoning:    r.Reasoning,
		Instructions: instructions,
	})
	if err := history.Append(taskName, history.Event{Type: "review.started", Data: json.RawMessage(startData)}); err != nil {
		return fmt.Errorf("failed to write review history: %w", err)
	}

	startedAt := time.Now()
	review, reviewErr := h.Review(cwd, target, instructions)
	durationMS := int(time.Since(startedAt).Milliseconds())

	if reviewErr != nil {
		errData, _ := json.Marshal(reviewFinishedData{
			RunID:      runID,
			Kind:       "diff",
			DurationMS: durationMS,
			Outcome:    "error",
			Error:      reviewErr.Error(),
		})
		if err := history.Append(taskName, history.Event{Type: "review.finished", Data: json.RawMessage(errData)}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write review history: %v\n", err)
		}
		return reviewErr
	}

	relPath, persistErr := persistReviewFile(taskName, "diff", r.Adapter, runID, review)
	if persistErr != nil {
		errData, _ := json.Marshal(reviewFinishedData{
			RunID:      runID,
			Kind:       "diff",
			DurationMS: durationMS,
			Outcome:    "error",
			Error:      persistErr.Error(),
		})
		if err := history.Append(taskName, history.Event{Type: "review.finished", Data: json.RawMessage(errData)}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write review history: %v\n", err)
		}
		fmt.Println(review)
		return fmt.Errorf("failed to persist review file: %w", persistErr)
	}

	finData, _ := json.Marshal(reviewFinishedData{
		RunID:      runID,
		Kind:       "diff",
		DurationMS: durationMS,
		Outcome:    "success",
		File:       relPath,
	})
	if err := history.Append(taskName, history.Event{Type: history.EventTypeReviewFinished, Data: json.RawMessage(finData)}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write review history: %v\n", err)
	}

	artData, _ := json.Marshal(history.ArtifactProducedData{
		Name: filepath.Base(relPath),
		Path: relPath,
		Kind: "review",
	})
	if err := history.Append(taskName, history.Event{Type: history.EventTypeArtifactProduced, Data: json.RawMessage(artData)}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write artifact history: %v\n", err)
	}

	fmt.Println(review)
	return nil
}

// runPlanReview reviews PLAN.md against the task's spec (TASK.md description).
// Catches plan-vs-spec drift before the worker implements something the lead
// already approved that didn't match the spec.
func (c *ReviewCmd) runPlanReview(cfg *workspace.Config, t *task.Task, r workspace.Resolved, instructions string) error {
	taskName := strings.TrimSpace(c.Task)
	taskDir := task.Dir(taskName)

	planPath := filepath.Join(taskDir, "PLAN.md")
	planData, err := os.ReadFile(planPath)
	if err != nil {
		return fmt.Errorf("PLAN.md not found for task %q at %s\n\nThe worker drafts PLAN.md during the plan stage. Wait for it before reviewing.", taskName, planPath)
	}
	plan := strings.TrimSpace(string(planData))
	if plan == "" {
		return fmt.Errorf("PLAN.md is empty for task %q", taskName)
	}

	spec := strings.TrimSpace(t.Description)
	if spec == "" {
		return fmt.Errorf("task %q has no description (TASK.md spec); nothing to review the plan against", taskName)
	}

	// Run in the workspace if one exists; otherwise the project root, since
	// plan review is documentation-only and doesn't need the worktree.
	cwd := ""
	if state, err := task.LoadState(taskName); err == nil && state != nil {
		cwd = state.Workspace
	}
	if cwd == "" {
		if root, err := os.Getwd(); err == nil {
			cwd = root
		}
	}

	prompt := buildPlanReviewPrompt(taskName, spec, plan, instructions)

	var h harness.Harness
	if c.testHarness != nil {
		h = c.testHarness
	} else {
		var err error
		h, err = harness.New(workspace.ConfigWithOverrides(cfg, r.Adapter, r.Provider, r.Model, r.Reasoning))
		if err != nil {
			return err
		}
	}

	// Append review.started before dispatching.
	runID, _ := history.NewRunID()

	startData, _ := json.Marshal(reviewStartedData{
		RunID:        runID,
		Kind:         "plan",
		Adapter:      r.Adapter,
		Model:        r.Model,
		Reasoning:    r.Reasoning,
		Instructions: instructions,
	})
	if err := history.Append(taskName, history.Event{Type: "review.started", Data: json.RawMessage(startData)}); err != nil {
		return fmt.Errorf("failed to write review history: %w", err)
	}

	startedAt := time.Now()
	result, reviewErr := h.Run(context.Background(), cwd, prompt, "", harness.Callbacks{})
	durationMS := int(time.Since(startedAt).Milliseconds())

	if reviewErr != nil {
		errData, _ := json.Marshal(reviewFinishedData{
			RunID:      runID,
			Kind:       "plan",
			DurationMS: durationMS,
			Outcome:    "error",
			Error:      reviewErr.Error(),
		})
		if err := history.Append(taskName, history.Event{Type: "review.finished", Data: json.RawMessage(errData)}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write review history: %v\n", err)
		}
		return reviewErr
	}

	reply := result.Reply
	relPath, persistErr := persistReviewFile(taskName, "plan", r.Adapter, runID, reply)
	if persistErr != nil {
		errData, _ := json.Marshal(reviewFinishedData{
			RunID:      runID,
			Kind:       "plan",
			DurationMS: durationMS,
			Outcome:    "error",
			Error:      persistErr.Error(),
		})
		if err := history.Append(taskName, history.Event{Type: "review.finished", Data: json.RawMessage(errData)}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write review history: %v\n", err)
		}
		fmt.Println(reply)
		return fmt.Errorf("failed to persist review file: %w", persistErr)
	}

	finData, _ := json.Marshal(reviewFinishedData{
		RunID:      runID,
		Kind:       "plan",
		DurationMS: durationMS,
		Outcome:    "success",
		File:       relPath,
	})
	if err := history.Append(taskName, history.Event{Type: history.EventTypeReviewFinished, Data: json.RawMessage(finData)}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write review history: %v\n", err)
	}

	artData, _ := json.Marshal(history.ArtifactProducedData{
		Name: filepath.Base(relPath),
		Path: relPath,
		Kind: "review",
	})
	if err := history.Append(taskName, history.Event{Type: history.EventTypeArtifactProduced, Data: json.RawMessage(artData)}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write artifact history: %v\n", err)
	}

	fmt.Println(reply)
	return nil
}

// buildPlanReviewPrompt frames PLAN.md as the artifact under review and
// TASK.md description as the spec. The wording mirrors the diff-review
// pattern: prioritized, actionable findings.
func buildPlanReviewPrompt(taskName, spec, plan, instructions string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Review PLAN.md for subtask task %q against the spec. ", taskName)
	sb.WriteString("Find drift between the spec's intent and the plan's steps: missing requirements, scope shrinkage, ambiguous handling, over-engineering, or steps that don't trace back to a spec line. Provide prioritized, actionable findings. Be concrete: cite plan section vs spec line.\n\n")
	sb.WriteString("## Spec (from TASK.md)\n\n")
	sb.WriteString(spec)
	sb.WriteString("\n\n## Plan (from PLAN.md)\n\n")
	sb.WriteString(plan)
	if instructions != "" {
		sb.WriteString("\n\n## Additional instructions\n\n")
		sb.WriteString(instructions)
	}
	return sb.String()
}
