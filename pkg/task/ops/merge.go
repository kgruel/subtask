package ops

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
)

type MergeResult struct {
	AlreadyClosed bool
	AlreadyMerged bool
}

// MergeTask squashes task commits into the base branch and marks the task as merged (resting).
// message is used as the squash commit message header; commit subjects are appended.
func MergeTask(taskName, message string, logger Logger) (MergeResult, error) {
	var res MergeResult
	locked, err := task.TryWithLock(taskName, func() error {
		r, err := mergeTaskUnlocked(taskName, message, logger)
		res = r
		return err
	})
	if err != nil {
		return MergeResult{}, err
	}
	if !locked {
		return MergeResult{}, fmt.Errorf("task %q is busy (another operation is in progress); try again", taskName)
	}
	return res, nil
}

func mergeTaskUnlocked(taskName, message string, logger Logger) (MergeResult, error) {
	// Load state.
	state, err := task.LoadState(taskName)
	if err != nil {
		return MergeResult{}, err
	}
	if state == nil {
		return MergeResult{}, fmt.Errorf("task %q not found or never run", taskName)
	}

	// Check if running.
	if state.SupervisorPID != 0 && !state.IsStale() {
		return MergeResult{}, fmt.Errorf("task %q is still working\n\nWait for it to finish:\n  subtask list", taskName)
	}

	// Check if already merged/closed (history source of truth).
	tail, _ := history.Tail(taskName)
	switch tail.TaskStatus {
	case task.TaskStatusMerged:
		return MergeResult{AlreadyMerged: true}, nil
	case task.TaskStatusClosed:
		return MergeResult{AlreadyClosed: true}, nil
	}

	// Load task for base branch.
	t, err := task.Load(taskName)
	if err != nil {
		return MergeResult{}, err
	}

	ws := state.Workspace

	// Preconditions: workspace must be clean.
	clean, err := git.IsClean(ws)
	if err != nil {
		return MergeResult{}, fmt.Errorf("failed to check git status: %w", err)
	}
	if !clean {
		return MergeResult{}, fmt.Errorf("%s has uncommitted changes\n\n"+
			"Ask worker to commit or discard them:\n"+
			"  subtask send %s \"Commit your changes (or discard if unneeded).\"\n\n"+
			"To inspect: cd $(subtask workspace %s) && git status",
			taskName, taskName, taskName)
	}

	// Get merge base and commit subjects.
	mergeBase, err := git.MergeBase(ws, t.BaseBranch, "HEAD")
	if err != nil {
		return MergeResult{}, fmt.Errorf("failed to find merge base with %s: %w", t.BaseBranch, err)
	}

	branchHead := ""
	if out, err := git.Output(ws, "rev-parse", "HEAD"); err == nil {
		branchHead = strings.TrimSpace(out)
	}
	baseHead := ""
	if out, err := git.Output(ws, "rev-parse", t.BaseBranch); err == nil {
		baseHead = strings.TrimSpace(out)
	}

	// Compute frozen stats relative to a PR-style base for this branch state.
	// - Default: merge-base(base, head)
	// - If the branch tip is already reachable from base (ancestor), merge-base == head; in that case
	//   try fork-point (uses base reflog) and fall back to the task's stored base_commit.
	baseCommit := strings.TrimSpace(mergeBase)
	if baseCommit == branchHead && branchHead != "" {
		if fp, err := git.MergeBaseForkPoint(ws, t.BaseBranch, branchHead); err == nil && strings.TrimSpace(fp) != "" && git.CommitExists(ws, strings.TrimSpace(fp)) {
			baseCommit = strings.TrimSpace(fp)
		} else if strings.TrimSpace(tail.BaseCommit) != "" && git.CommitExists(ws, strings.TrimSpace(tail.BaseCommit)) {
			baseCommit = strings.TrimSpace(tail.BaseCommit)
		}
	}

	// If the task branch's content is already in base (e.g. squash merge, cherry-pick, or ancestor),
	// treat `subtask merge` as a no-op finalize: record task.merged and free the workspace.
	integrated := git.IsIntegrated(ws, "HEAD", t.BaseBranch)
	added := 0
	removed := 0
	commitCount := 0
	frozenErr := ""
	if baseCommit == "" || branchHead == "" {
		frozenErr = fmt.Sprintf("cannot compute frozen stats (missing base_commit=%t branch_head=%t)", baseCommit == "", branchHead == "")
	} else if !git.CommitExists(ws, baseCommit) {
		frozenErr = fmt.Sprintf("cannot compute frozen stats (missing base_commit %s)", baseCommit)
	} else if !git.CommitExists(ws, branchHead) {
		frozenErr = fmt.Sprintf("cannot compute frozen stats (missing branch_head %s)", branchHead)
	} else {
		if a, r, err := git.DiffStatRange(ws, baseCommit, branchHead); err == nil {
			added = a
			removed = r
		} else {
			frozenErr = fmt.Sprintf("cannot compute frozen stats: %v", err)
		}
		if frozenErr == "" {
			if n, err := git.RevListCount(ws, baseCommit, branchHead); err == nil {
				commitCount = n
			} else {
				frozenErr = fmt.Sprintf("cannot compute commit_count: %v", err)
			}
		}
	}

	if integrated != "" {
		logInfo(logger, fmt.Sprintf("Already in %s (%s). Marking task as merged...", t.BaseBranch, integrated))

		// Detach HEAD to free the workspace.
		taskBranch, _ := git.CurrentBranch(ws)
		if err := git.RunSilent(ws, "checkout", "--detach", "HEAD"); err != nil {
			logWarning(logger, fmt.Sprintf("failed to detach HEAD: %v", err))
		}

		// Delete task branch (cleanup).
		if taskBranch != "" && taskBranch != t.BaseBranch {
			if err := git.RunSilent(ws, "branch", "-D", taskBranch); err != nil {
				logWarning(logger, fmt.Sprintf("failed to delete branch %s: %v", taskBranch, err))
			}
		}

		mergedData := map[string]any{
			// Back-compat
			// No-op finalize: we didn't create a merge commit, so avoid pretending we did.
			// `subtask diff` can use base_commit..branch_head when the branch is deleted.
			"commit": "",
			"into":   t.BaseBranch,
			"branch": taskName,

			// Redesign fields
			"via":             "subtask",
			"method":          string(integrated),
			"base_branch":     t.BaseBranch,
			"base_commit":     baseCommit,
			"branch_head":     branchHead,
			"base_head":       baseHead,
			"target_head":     baseHead,
			"changes_added":   added,
			"changes_removed": removed,
			"commit_count":    commitCount,
			"detected_at":     time.Now().UTC().Unix(),
		}
		if frozenErr != "" {
			mergedData["frozen_error"] = frozenErr
		}
		data, _ := json.Marshal(mergedData)
		_ = history.AppendLocked(taskName, history.Event{Type: "task.merged", Data: data})

		// Clear runtime state.
		state.Workspace = ""
		state.SessionID = ""
		state.Adapter = ""
		state.SupervisorPID = 0
		state.SupervisorPGID = 0
		state.StartedAt = time.Time{}
		state.LastError = ""
		if err := state.Save(taskName); err != nil {
			return MergeResult{}, err
		}

		logSuccess(logger, fmt.Sprintf("Merged %s into %s. Workspace freed.", taskName, t.BaseBranch))
		return MergeResult{}, nil
	}

	// Preflight: detect conflicts the same way `git merge <base>` would fail.
	//
	// This avoids rewriting the task branch (squash) only to discover conflicts during integration.
	if conflicts, err := git.MergeConflictFiles(ws, t.BaseBranch, "HEAD"); err == nil && len(conflicts) > 0 {
		var b strings.Builder
		b.WriteString(fmt.Sprintf("merge failed: conflicts with %s\n\n", t.BaseBranch))
		b.WriteString("Cannot merge cleanly. Conflicting files:\n")
		for _, f := range conflicts {
			b.WriteString("  - ")
			b.WriteString(f)
			b.WriteString("\n")
		}
		b.WriteString("\nAsk worker to resolve:\n")
		b.WriteString(fmt.Sprintf("  subtask send %s \"Merge the local %s branch into your branch (git merge %s) and resolve conflicts\"\n\n", taskName, t.BaseBranch, t.BaseBranch))
		b.WriteString("Tip: If you know what changed on ")
		b.WriteString(t.BaseBranch)
		b.WriteString(", add context to help the worker\n")
		b.WriteString("preserve important changes (e.g., \"`main` added X, keep your Y changes\").\n\n")
		b.WriteString(fmt.Sprintf("Manual: cd $(subtask workspace %s) && git status", taskName))
		return MergeResult{}, fmt.Errorf("%s", strings.TrimSpace(b.String()))
	}

	subjects, err := git.GetCommitSubjects(ws, mergeBase)
	if err != nil {
		return MergeResult{}, fmt.Errorf("failed to get commit history: %w", err)
	}
	if len(subjects) == 0 {
		return MergeResult{}, fmt.Errorf("no commits to merge\n\n"+
			"The task branch has no new commits relative to %s",
			t.BaseBranch)
	}

	// Build the commit message: user message + commit subjects.
	var msgBuilder strings.Builder
	msgBuilder.WriteString(message)
	msgBuilder.WriteString("\n\n")
	for _, subj := range subjects {
		msgBuilder.WriteString("* ")
		msgBuilder.WriteString(subj)
		msgBuilder.WriteString("\n")
	}
	fullMessage := strings.TrimSpace(msgBuilder.String()) + "\n\nSubtask-Task: " + taskName

	// Squash commits.
	logInfo(logger, fmt.Sprintf("Squashing %d commits...", len(subjects)))
	if err := git.SquashCommits(ws, mergeBase, fullMessage); err != nil {
		return MergeResult{}, fmt.Errorf("squash failed: %w", err)
	}

	// Apply onto local base branch (local-only merge).
	logInfo(logger, fmt.Sprintf("Applying onto %s...", t.BaseBranch))
	if err := git.RebaseOnto(ws, t.BaseBranch); err != nil {
		return MergeResult{}, fmt.Errorf("merge failed: conflicts with %s\n\n"+
			"%v\n\n"+
			"Ask worker to resolve:\n"+
			"  subtask send %s \"Merge the local %s branch into your branch (git merge %s) and resolve conflicts\"\n\n"+
			"Tip: If you know what changed on %s, add context to help the worker\n"+
			"preserve important changes (e.g., \"`main` added X, keep your Y changes\").\n\n"+
			"Manual: cd $(subtask workspace %s) && git status",
			t.BaseBranch, err, taskName, t.BaseBranch, t.BaseBranch, t.BaseBranch, taskName)
	}

	// Fast-forward merge to local base branch.
	logInfo(logger, fmt.Sprintf("Updating %s...", t.BaseBranch))
	if err := git.LocalPush(ws, t.BaseBranch); err != nil {
		return MergeResult{}, fmt.Errorf("failed to update %s: %w", t.BaseBranch, err)
	}
	mergedCommit, _ := git.Output(ws, "rev-parse", t.BaseBranch)

	// Detach HEAD to free the workspace.
	taskBranch, _ := git.CurrentBranch(ws)
	if err := git.RunSilent(ws, "checkout", "--detach", "HEAD"); err != nil {
		logWarning(logger, fmt.Sprintf("failed to detach HEAD: %v", err))
	}

	// Delete task branch (cleanup). Use -D (force delete) since we just merged successfully.
	if taskBranch != "" && taskBranch != t.BaseBranch {
		if err := git.RunSilent(ws, "branch", "-D", taskBranch); err != nil {
			logWarning(logger, fmt.Sprintf("failed to delete branch %s: %v", taskBranch, err))
		}
	}

	// Append history event and clear runtime state.
	mergedData := map[string]any{
		"commit":          strings.TrimSpace(mergedCommit),
		"into":            t.BaseBranch,
		"branch":          taskName,
		"merge_base":      mergeBase,
		"via":             "subtask",
		"method":          "squash",
		"base_branch":     t.BaseBranch,
		"base_commit":     baseCommit,
		"branch_head":     branchHead,
		"changes_added":   added,
		"changes_removed": removed,
		"commit_count":    commitCount,
		"trailers": map[string]string{
			"Subtask-Task": taskName,
		},
	}
	if frozenErr != "" {
		mergedData["frozen_error"] = frozenErr
	}
	data, _ := json.Marshal(mergedData)
	_ = history.AppendLocked(taskName, history.Event{Type: "task.merged", Data: data})

	state.Workspace = ""
	state.SessionID = ""
	state.Adapter = ""
	state.SupervisorPID = 0
	state.SupervisorPGID = 0
	state.StartedAt = time.Time{}
	state.LastError = ""
	if err := state.Save(taskName); err != nil {
		return MergeResult{}, err
	}

	logSuccess(logger, fmt.Sprintf("Merged %s into %s. Workspace freed.", taskName, t.BaseBranch))
	return MergeResult{}, nil
}
