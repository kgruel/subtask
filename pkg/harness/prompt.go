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

	// The active routine step drives per-step blocks (agent, stage, inputs).
	// Resolved once: the stamped stage, or the routine's entry step on first send.
	activeStep := stage
	if rt != nil && activeStep == "" {
		activeStep = rt.EntryStep()
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

	// Follow-up parent artifacts: durable, artifacts-first background for a
	// child seeded from a merged/closed (or never-dispatched) parent whose
	// session can't be duplicated. Renders nothing when there is no parent or
	// no readable parent files, so non-follow-up prompts stay byte-identical.
	if pc := renderParentContext(t.FollowUp); pc != "" {
		sb.WriteString(pc)
	}

	// Routine per-step worker_instructions / worker_context, when in a
	// routine task. Mirrors the workflow stage block below but reads
	// from the current routine step.
	if rt != nil {
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

	// Routine per-step consumes: → `## Inputs`. Keyed on len(consumes) > 0
	// alone (independent of `## Stage`); load-time validation guarantees only
	// regular steps carry a consumes list, so terminal/gate steps render none.
	if rt != nil {
		if s := rt.GetStep(activeStep); s != nil {
			if in := renderConsumedInputs(taskDir, task.Dir(t.Name), s.Consumes); in != "" {
				sb.WriteString(in)
			}
		}
	}

	// Separator and prompt
	sb.WriteString("\n--------------------\n\n")
	sb.WriteString(prompt)
	return sb.String(), nil
}

// renderConsumedInputs lists the active step's consumes: artifacts as
// workspace-relative paths (the form the worker can open through the task-folder
// symlink), existence-checked. Missing entries are marked so the worker knows an
// expected input is absent. taskDirSlash is BuildPrompt's already-computed
// filepath.ToSlash(task.Dir(t.Name)); nativeTaskDir is task.Dir(t.Name). Paths
// are trusted to be task-relative and non-escaping — validateArtifactPath
// enforced that at routine load, so no re-validation happens here.
func renderConsumedInputs(taskDirSlash, nativeTaskDir string, consumes []string) string {
	if len(consumes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Inputs\n")
	b.WriteString("Artifacts this step consumes (read them before starting):\n")
	any := false
	for _, rel := range consumes {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		disp := taskDirSlash + "/" + filepath.ToSlash(rel)
		abs := filepath.Join(nativeTaskDir, filepath.FromSlash(rel))
		switch fi, err := os.Stat(abs); {
		case err == nil && fi.IsDir():
			fmt.Fprintf(&b, "- %s (directory)\n", disp)
		case err == nil:
			fmt.Fprintf(&b, "- %s\n", disp)
		default:
			fmt.Fprintf(&b, "- %s (missing — expected input not found)\n", disp)
		}
		any = true
	}
	if !any {
		return ""
	}
	return b.String()
}

// renderParentContext lists a follow-up parent's readable artifacts as absolute
// paths so a child seeded from a merged/closed parent (whose session can't be
// duplicated) still gets durable, artifacts-first background. Returns "" when
// parent is empty or has no readable files (which keeps non-parent prompts and
// parent-less follow-up goldens byte-identical).
func renderParentContext(parent string) string {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return ""
	}
	absDir := task.DirAbs(parent)

	type ref struct{ label, abs string }
	var refs []ref
	seen := make(map[string]struct{}) // keyed by slash relpath, dedups the explicit PROGRESS.json append

	arts, _ := task.Artifacts(parent) // best-effort; nil on error
	for _, a := range arts {
		if a.Missing {
			continue
		}
		rel := filepath.ToSlash(a.Path)
		seen[rel] = struct{}{}
		label := a.Name
		if label == "" {
			label = a.Path
		}
		refs = append(refs, ref{label: label, abs: filepath.Join(absDir, filepath.FromSlash(a.Path))})
	}
	// PROGRESS.json is not returned by task.Artifacts (its well-known set is only
	// TASK.md/PLAN.md); include it if present. Skip when a step's produces: is
	// literally "PROGRESS.json" (already emitted above) so it isn't listed twice.
	if _, dup := seen["PROGRESS.json"]; !dup {
		if progAbs := filepath.Join(absDir, "PROGRESS.json"); fileExists(progAbs) {
			refs = append(refs, ref{label: "PROGRESS.json", abs: progAbs})
		}
	}
	if len(refs) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n## Parent Context\n")
	fmt.Fprintf(&b, "This task is a follow-up from %s. Read these files for background (absolute paths, read-only):\n", parent)
	for _, r := range refs {
		fmt.Fprintf(&b, "- %s: %s\n", r.label, filepath.ToSlash(r.abs))
	}
	return b.String()
}

// fileExists reports whether path is an existing non-directory file.
func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}
