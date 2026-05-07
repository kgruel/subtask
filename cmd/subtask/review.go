package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/kgruel/subtask/pkg/harness"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/workspace"
)

// ReviewCmd implements 'subtask review'.
type ReviewCmd struct {
	// Target selection (mutually exclusive)
	Task        string `help:"Review changes in a task workspace against that task's base branch"`
	Base        string `help:"Review changes on the current branch against BRANCH (PR-style diff via merge-base; BRANCH must be a valid git ref)"`
	Uncommitted bool   `help:"Review uncommitted changes (staged, unstaged, untracked)"`
	Commit      string `help:"Review changes introduced by a specific commit SHA"`

	// Optional instructions
	Prompt string `arg:"" optional:"" help:"Additional review instructions (or use stdin)"`

	// Adapter/model/reasoning overrides (do not persist)
	Adapter   string `help:"Override adapter for this review (does not persist)"`
	Preset    string `help:"Preset shorthand for adapter/model/reasoning (does not persist)"`
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

	// Resolve adapter/model/reasoning — mirror send.go preset resolution.
	// Precedence: explicit flags > --preset > task snapshot (when --task) > project default.
	r, err := workspace.Resolve(cfg, t, workspace.ResolveOverrides{
		Adapter:   c.Adapter,
		Model:     c.Model,
		Reasoning: c.Reasoning,
		Preset:    c.Preset,
	})
	if err != nil {
		return err
	}

	// Determine working directory and target
	var cwd string
	var target harness.ReviewTarget

	switch {
	case strings.TrimSpace(c.Task) != "":
		taskName := strings.TrimSpace(c.Task)
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

	// Run review
	var h harness.Harness
	if c.testHarness != nil {
		h = c.testHarness
	} else {
		h, err = harness.New(workspace.ConfigWithOverrides(cfg, r.Adapter, r.Provider, r.Model, r.Reasoning))
		if err != nil {
			return err
		}
	}

	review, err := h.Review(cwd, target, instructions)
	if err != nil {
		return err
	}

	fmt.Println(review)
	return nil
}

