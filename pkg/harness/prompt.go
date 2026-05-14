package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kgruel/subtask/pkg/agent"
	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
)

type RepoStatus struct {
	ConflictFiles []string
}

func FormatRepoStatusWarning(baseBranch string, status *RepoStatus) string {
	if status == nil {
		return ""
	}
	var lines []string

	if len(status.ConflictFiles) > 0 {
		lines = append(lines, fmt.Sprintf(
			"Note: This branch conflicts with %s in: %s. Consider running `git merge %s` to resolve.",
			baseBranch,
			strings.Join(status.ConflictFiles, ", "),
			baseBranch,
		))
	}

	return strings.Join(lines, "\n")
}

// BuildPrompt creates the full prompt with header.
//
// stage is the task's current routine step name (from history.Tail). When
// non-empty and the routine defines worker_instructions for that step, the
// instructions are appended. Pass "" if the task has no routine or no
// recorded stage transitions yet.
func BuildPrompt(t *task.Task, workspace string, sameWorkspace bool, stage string, prompt string, status *RepoStatus) (string, error) {
	var sb strings.Builder
	taskDir := filepath.ToSlash(task.Dir(t.Name))

	// Header
	sb.WriteString("# Task\n")
	fmt.Fprintf(&sb, "Name: %s\n", t.Name)
	fmt.Fprintf(&sb, "Title: %s\n", t.Title)
	fmt.Fprintf(&sb, "Branch: %s\n", t.Name)

	// List non-builtin files in task directory
	var extraFiles []string
	if entries, err := os.ReadDir(taskDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			switch e.Name() {
			case "TASK.md", "history.jsonl",
				"WORKFLOW.yaml": // orphan in pre-routine task folders; skip from extras
				continue
			}
			extraFiles = append(extraFiles, e.Name())
		}
	}
	if len(extraFiles) > 0 {
		fmt.Fprintf(&sb, "Directory: %s (%s)\n", taskDir, strings.Join(extraFiles, ", "))
	} else {
		fmt.Fprintf(&sb, "Directory: %s\n", taskDir)
	}

	// Follow-up continuation note
	if t.FollowUp != "" {
		fmt.Fprintf(&sb, "Follow-up: continuing from %s\n", t.FollowUp)
		if !sameWorkspace {
			sb.WriteString("Note: New workspace, branch checked out fresh.\n")
		}
	}

	// Staleness/conflict warnings (optional).
	if warn := FormatRepoStatusWarning(t.BaseBranch, status); warn != "" {
		sb.WriteString(warn)
		sb.WriteString("\n")
	}

	// Workspace orientation: name the worktree explicitly and forbid editing
	// outside it. Without this the LLM may infer (from a workspace path that
	// contains "subtask") that cwd is orchestration infrastructure and the
	// "real" project lives at a sibling absolute path — leading to edits
	// outside the worktree that won't appear on the task branch and won't
	// merge. Skipped when workspace is empty (e.g. tests building prompts in
	// isolation).
	if workspace != "" {
		sb.WriteString("\n## Workspace\n")
		fmt.Fprintf(&sb, "Your working directory is `%s`. This IS your copy of the project — a git worktree of `%s` on branch `%s`.\n", workspace, t.BaseBranch, t.Name)
		sb.WriteString("\n")
		sb.WriteString("- Use paths relative to cwd, or absolute paths under cwd.\n")
		sb.WriteString("- Never use absolute paths to other clones of this project (e.g. `/Users/.../Code/<projectname>`). Edits outside your worktree will not appear on your task branch and will not merge.\n")
		sb.WriteString("- If unsure, run `pwd` and `git rev-parse --show-toplevel` — both should match cwd.\n")
	}

	// Routine tasks emit `## Project` from routine.default_prompt; non-routine
	// tasks emit no `## Project` block. `## Agent` comes from the current
	// routine step's `agent:` field for routine tasks; for non-routine tasks
	// it comes from t.Agent (no `## Project` block in either case).
	var rt *routine.Routine
	if t.Routine != "" {
		r, err := routine.LoadByName(t.Routine)
		if err != nil {
			return "", err
		}
		rt = r
	}

	if rt != nil {
		// `## Project` from routine.default_prompt.
		body, err := rt.ResolveDefaultPromptText()
		if err != nil {
			return "", err
		}
		body = strings.TrimSpace(body)
		if body != "" {
			sb.WriteString("\n## Project\n")
			sb.WriteString(body)
			sb.WriteString("\n")
		}
	}

	// Agent role prompt. Re-resolved every build so prompt-file edits
	// land without redrafting. Layering: routine.default_prompt →
	// agent.prompt → per-task message.
	//
	// Routine tasks pick the agent per step (rt.GetStep(stage).Agent).
	// A preset-only step (no agent) emits NO `## Agent` block — t.Agent
	// is not a fallback for routine tasks.
	agentName := ""
	if rt != nil {
		activeStep := stage
		if activeStep == "" {
			activeStep = rt.EntryStep()
		}
		if s := rt.GetStep(activeStep); s != nil {
			agentName = s.Agent
		}
	} else {
		agentName = t.Agent
	}
	if agentName != "" {
		ag, err := agent.LoadByName(agentName)
		if err != nil {
			return "", err
		}
		body, err := ag.ResolvePromptText()
		if err != nil {
			return "", err
		}
		body = strings.TrimSpace(body)
		if body != "" {
			sb.WriteString("\n## Agent\n")
			sb.WriteString(body)
			sb.WriteString("\n")
		}
	}

	// Description
	if t.Description != "" {
		sb.WriteString("\n## Description\n")
		sb.WriteString(t.Description)
		sb.WriteString("\n")
	}

	// Routine per-step worker_instructions / worker_context, when in a
	// routine task. Mirrors the workflow stage block below but reads
	// from the current routine step.
	if rt != nil {
		activeStep := stage
		if activeStep == "" {
			activeStep = rt.EntryStep()
		}
		if s := rt.GetStep(activeStep); s != nil {
			wi := strings.TrimSpace(s.WorkerInstructions)
			wc := strings.TrimSpace(s.WorkerContext)
			if wi != "" || wc != "" {
				fmt.Fprintf(&sb, "\n## Stage: %s\n", activeStep)
				if wi != "" {
					sb.WriteString(wi)
					sb.WriteString("\n")
				}
				if wc != "" {
					if wi != "" {
						sb.WriteString("\n")
					}
					sb.WriteString(wc)
					sb.WriteString("\n")
				}
			}
		}
	}

	// Separator and prompt
	sb.WriteString("\n--------------------\n\n")
	sb.WriteString(prompt)
	return sb.String(), nil
}
