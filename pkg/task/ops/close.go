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

type CloseResult struct {
	AlreadyClosed bool
}

// CloseTask closes a task and frees its workspace.
// If abandon is true, uncommitted changes are discarded.
func CloseTask(taskName string, abandon bool, logger Logger) (CloseResult, error) {
	var res CloseResult
	locked, err := task.TryWithLock(taskName, func() error {
		tail, _ := history.Tail(taskName)
		if tail.TaskStatus == task.TaskStatusClosed {
			res.AlreadyClosed = true
			return nil
		}

		t, _ := task.Load(taskName) // best-effort (allows closing synced tasks without full metadata)
		repoDir := task.ProjectRoot()

		state, err := task.LoadState(taskName)
		if err != nil {
			return err
		}
		// A synced task may have no local state.json; closing is still allowed.
		if state == nil {
			state = &task.State{}
		}

		// Check if running.
		if state.SupervisorPID != 0 && !state.IsStale() {
			return fmt.Errorf("task %q is still working\n\nWait for it to finish:\n  subtask list", taskName)
		}

		// Check for clean state.
		if !abandon {
			if strings.TrimSpace(state.Workspace) != "" {
				clean, err := git.IsClean(state.Workspace)
				if err != nil {
					return fmt.Errorf("failed to check git status: %w", err)
				}
				if !clean {
					return fmt.Errorf("%s has uncommitted changes\n\n"+
						"To inspect: cd $(subtask workspace %s) && git status\n"+
						"To discard: subtask close %s --abandon",
						taskName, taskName, taskName)
				}
			}
		}

		// If abandoning, reset the workspace.
		if abandon {
			if strings.TrimSpace(state.Workspace) != "" {
				if err := git.ResetHard(state.Workspace); err != nil {
					return fmt.Errorf("failed to reset workspace: %w", err)
				}
			}
		}

		// Detach HEAD to free the workspace.
		if strings.TrimSpace(state.Workspace) != "" {
			if err := git.RunSilent(state.Workspace, "checkout", "--detach", "HEAD"); err != nil {
				logWarning(logger, fmt.Sprintf("failed to detach HEAD: %v", err))
			}
		}

		// Best-effort: delete empty task branch (no unique commits).
		// This keeps the repo clean for tasks that were never started.
		if strings.TrimSpace(state.Workspace) != "" && t != nil && strings.TrimSpace(t.BaseBranch) != "" {
			deleteEmptyTaskBranchBestEffort(logger, state.Workspace, taskName, t.BaseBranch)
		}

		baseBranch := ""
		if t != nil {
			baseBranch = strings.TrimSpace(t.BaseBranch)
		}
		if baseBranch == "" {
			baseBranch = strings.TrimSpace(tail.BaseBranch)
		}
		baseCommit := strings.TrimSpace(tail.BaseCommit)

		branchHead := ""
		// Prefer the workspace's HEAD if it exists (covers cases where the branch name ref is missing).
		if strings.TrimSpace(state.Workspace) != "" {
			if out, err := git.Output(state.Workspace, "rev-parse", "HEAD"); err == nil {
				branchHead = strings.TrimSpace(out)
			}
		}
		if branchHead == "" && git.BranchExists(repoDir, taskName) {
			if out, err := git.Output(repoDir, "rev-parse", taskName); err == nil {
				branchHead = strings.TrimSpace(out)
			}
		}
		statsDir := repoDir
		if strings.TrimSpace(state.Workspace) != "" {
			statsDir = state.Workspace
		}

		// Compute frozen stats relative to a PR-style base for this branch state.
		// Use merge-base(base, head) when possible, so rebases don't inflate stats.
		if strings.TrimSpace(baseBranch) != "" && strings.TrimSpace(branchHead) != "" && git.CommitExists(statsDir, branchHead) {
			if mb, err := git.MergeBase(statsDir, baseBranch, branchHead); err == nil && strings.TrimSpace(mb) != "" {
				baseCommit = strings.TrimSpace(mb)
			}
		}

		// Back-compat: older tasks may not have base_commit; fall back to merge-base when possible.
		if baseCommit == "" && baseBranch != "" && (branchHead != "" || git.BranchExists(repoDir, taskName)) {
			mbDir := repoDir
			mbBranch := taskName
			if strings.TrimSpace(state.Workspace) != "" {
				mbDir = state.Workspace
				mbBranch = "HEAD"
			}
			if mb, err := git.Output(mbDir, "merge-base", mbBranch, baseBranch); err == nil {
				baseCommit = strings.TrimSpace(mb)
			}
		}

		added := 0
		removed := 0
		commitCount := 0
		frozenErr := ""
		if baseCommit == "" || branchHead == "" {
			frozenErr = fmt.Sprintf("cannot compute frozen stats (missing base_commit=%t branch_head=%t)", baseCommit == "", branchHead == "")
		} else if !git.CommitExists(statsDir, baseCommit) {
			frozenErr = fmt.Sprintf("cannot compute frozen stats (missing base_commit %s)", baseCommit)
		} else if !git.CommitExists(statsDir, branchHead) {
			frozenErr = fmt.Sprintf("cannot compute frozen stats (missing branch_head %s)", branchHead)
		} else {
			if a, r, err := git.DiffStatRange(statsDir, baseCommit, branchHead); err == nil {
				added = a
				removed = r
			} else {
				frozenErr = fmt.Sprintf("cannot compute frozen stats: %v", err)
			}
			if frozenErr == "" {
				if n, err := git.RevListCount(statsDir, baseCommit, branchHead); err == nil {
					commitCount = n
				} else {
					frozenErr = fmt.Sprintf("cannot compute commit_count: %v", err)
				}
			}
		}

		// Append history event.
		reason := "close"
		if abandon {
			reason = "abandon"
		}
		closedData := map[string]any{
			"reason":          reason,
			"base_branch":     baseBranch,
			"base_commit":     baseCommit,
			"branch_head":     branchHead,
			"changes_added":   added,
			"changes_removed": removed,
			"commit_count":    commitCount,
		}
		if frozenErr != "" {
			closedData["frozen_error"] = frozenErr
		}
		data, _ := json.Marshal(closedData)
		_ = history.AppendLocked(taskName, history.Event{Type: "task.closed", Data: data, TS: time.Now().UTC()})

		// Clear runtime state.
		state.Workspace = ""
		state.SessionID = ""
		state.Adapter = ""
		state.SupervisorPID = 0
		state.SupervisorPGID = 0
		state.StartedAt = time.Time{}
		state.LastError = ""
		if err := state.Save(taskName); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return CloseResult{}, err
	}
	if !locked {
		return CloseResult{}, fmt.Errorf("task %q is busy (another operation is in progress); try again", taskName)
	}
	return res, nil
}

func deleteEmptyTaskBranchBestEffort(logger Logger, repoDir, branch, baseBranch string) {
	branch = strings.TrimSpace(branch)
	baseBranch = strings.TrimSpace(baseBranch)
	if repoDir == "" || branch == "" || baseBranch == "" {
		return
	}
	// Never delete the base branch.
	if branch == baseBranch {
		return
	}
	if !git.BranchExists(repoDir, branch) {
		return
	}

	// Local-first: compare against the local base branch only.
	target := baseBranch
	if !git.BranchExists(repoDir, target) {
		return
	}

	mb, err := git.Output(repoDir, "merge-base", branch, target)
	if err != nil {
		return
	}
	head, err := git.Output(repoDir, "rev-parse", branch)
	if err != nil {
		return
	}
	if strings.TrimSpace(mb) != strings.TrimSpace(head) {
		return
	}

	// If the branch is checked out in another worktree, this will fail. Ignore errors.
	if err := git.RunSilent(repoDir, "branch", "-D", branch); err != nil {
		logWarning(logger, fmt.Sprintf("failed to delete empty branch %q: %v", branch, err))
	}
}
