package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/workflow"
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
// stage is the task's current workflow stage name (from history.Tail). When
// non-empty and the workflow defines worker_instructions for that stage, the
// instructions are appended after the workflow.worker block. Pass "" if the
// task has no workflow or no recorded stage transitions yet.
func BuildPrompt(t *task.Task, workspace string, sameWorkspace bool, stage string, prompt string, status *RepoStatus) string {
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
			case "TASK.md", "WORKFLOW.yaml", "history.jsonl":
				continue // built-in files
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

	// Description
	if t.Description != "" {
		sb.WriteString("\n## Description\n")
		sb.WriteString(t.Description)
		sb.WriteString("\n")
	}

	// Workflow instructions (workflow-wide, then per-stage if any)
	if wf, err := workflow.LoadFromTask(t.Name); err == nil && wf != nil {
		if wf.Instructions.Worker != "" {
			sb.WriteString("\n## Workflow\n")
			sb.WriteString(strings.TrimSpace(wf.Instructions.Worker))
			sb.WriteString("\n")
		}
		// Empty stage means "implicit first stage" — same convention as
		// cmd/subtask/stage.go uses when no stage.changed event has fired yet.
		activeStage := stage
		if activeStage == "" {
			activeStage = wf.FirstStage()
		}
		if s := wf.GetStage(activeStage); s != nil && s.WorkerInstructions != "" {
			fmt.Fprintf(&sb, "\n## Stage: %s\n", activeStage)
			sb.WriteString(strings.TrimSpace(s.WorkerInstructions))
			sb.WriteString("\n")
		}
	}

	// Separator and prompt
	sb.WriteString("\n--------------------\n\n")
	sb.WriteString(prompt)
	return sb.String()
}
