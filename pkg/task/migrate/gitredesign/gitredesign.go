package gitredesign

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/logging"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	taskmigrate "github.com/zippoxer/subtask/pkg/task/migrate"
)

// TaskSchemaVersion is the task schema version that indicates the git redesign migration
// has been applied (best-effort) and can be skipped on subsequent runs.
//
// v0.1.1 tasks commonly have schema=1 (schema1 history.jsonl). This migration upgrades
// them to schema=2 by backfilling missing git redesign fields.
const TaskSchemaVersion = 2

// Ensure performs a best-effort, idempotent migration to support the git redesign:
// - Backfills missing base_commit in the most recent task.opened event.
// - Backfills frozen change stats in task.merged / task.closed events when missing.
//
// It is safe to call multiple times; if tasks are already migrated it becomes a no-op.
func Ensure(repoDir string) error {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return nil
	}

	taskNames, err := task.List()
	if err != nil {
		return err
	}
	if len(taskNames) == 0 {
		return nil
	}

	for _, name := range taskNames {
		// Fast path: schema already indicates the redesign migration has been applied.
		// This avoids per-task locks and full history parses on every CLI command.
		t, err := task.Load(name)
		if err == nil && t != nil && t.Schema >= TaskSchemaVersion {
			continue
		}

		// Ensure schema/history exist (locks internally).
		if err := taskmigrate.EnsureSchema(name); err != nil {
			logging.Error("migrate", fmt.Sprintf("gitredesign ensure schema task=%s err=%v", name, err))
			continue
		}
		if err := migrateTask(repoDir, name); err != nil {
			logging.Error("migrate", fmt.Sprintf("gitredesign task=%s err=%v", name, err))
			continue
		}

		// Mark as migrated so subsequent runs can skip this task entirely.
		if err := bumpTaskSchema(name, TaskSchemaVersion); err != nil {
			logging.Error("migrate", fmt.Sprintf("gitredesign bump schema task=%s err=%v", name, err))
		}
	}

	return nil
}

func bumpTaskSchema(taskName string, version int) error {
	return task.WithLock(taskName, func() error {
		t, err := task.Load(taskName)
		if err != nil || t == nil {
			return nil
		}
		if t.Schema >= version {
			return nil
		}
		t.Schema = version
		return t.Save()
	})
}

func migrateTask(repoDir, taskName string) error {
	t, err := task.Load(taskName)
	if err != nil {
		return nil
	}

	return task.WithLock(taskName, func() error {
		events, err := history.Read(taskName, history.ReadOptions{})
		if err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}

		dirty := false

		openedIdx := lastIndexOfType(events, "task.opened")
		openedData := map[string]any{}
		if openedIdx >= 0 {
			_ = json.Unmarshal(events[openedIdx].Data, &openedData)

			if strings.TrimSpace(getString(openedData, "base_commit")) == "" {
				baseBranch := strings.TrimSpace(getString(openedData, "base_branch"))
				if baseBranch == "" {
					baseBranch = strings.TrimSpace(t.BaseBranch)
				}
				baseCommit := inferBaseCommit(repoDir, taskName, baseBranch)
				if baseCommit != "" {
					openedData["base_commit"] = baseCommit
					openedData["base_ref"] = baseBranch
					if b, err := json.Marshal(openedData); err == nil {
						events[openedIdx].Data = b
						dirty = true
					}
				}
			}
		}

		// Best-effort: backfill frozen stats for merged tasks when missing.
		mergedIdx := lastIndexOfType(events, "task.merged")
		if mergedIdx >= 0 {
			data := map[string]any{}
			_ = json.Unmarshal(events[mergedIdx].Data, &data)
			if _, ok := data["changes_added"]; !ok {
				commit := strings.TrimSpace(getString(data, "commit"))
				added, removed, frozenErr := inferFrozenStatsForMerge(repoDir, commit)
				if frozenErr != "" {
					data["frozen_error"] = frozenErr
				} else {
					data["changes_added"] = added
					data["changes_removed"] = removed
				}
				if b, err := json.Marshal(data); err == nil {
					events[mergedIdx].Data = b
					dirty = true
				}
			}
		}

		// Best-effort: backfill frozen stats for closed tasks when missing.
		closedIdx := lastIndexOfType(events, "task.closed")
		if closedIdx >= 0 {
			data := map[string]any{}
			_ = json.Unmarshal(events[closedIdx].Data, &data)
			if _, ok := data["changes_added"]; !ok {
				baseCommit := strings.TrimSpace(getString(openedData, "base_commit"))
				if baseCommit == "" {
					baseBranch := strings.TrimSpace(t.BaseBranch)
					if baseBranch == "" {
						baseBranch = strings.TrimSpace(getString(openedData, "base_branch"))
					}
					mb := inferBaseCommit(repoDir, taskName, baseBranch)
					if mb != "" {
						baseCommit = mb
					}
				}

				branchHead := ""
				if git.BranchExists(repoDir, taskName) {
					if out, err := git.Output(repoDir, "rev-parse", taskName); err == nil {
						branchHead = strings.TrimSpace(out)
					}
				}
				added, removed, commitCount, frozenErr := inferFrozenStatsForClose(repoDir, baseCommit, branchHead)
				data["base_branch"] = strings.TrimSpace(t.BaseBranch)
				data["base_commit"] = baseCommit
				data["branch_head"] = branchHead
				if frozenErr != "" {
					data["frozen_error"] = frozenErr
				} else {
					data["changes_added"] = added
					data["changes_removed"] = removed
					data["commit_count"] = commitCount
				}
				if b, err := json.Marshal(data); err == nil {
					events[closedIdx].Data = b
					dirty = true
				}
			}
		}

		if !dirty {
			return nil
		}
		return history.WriteAllLocked(taskName, events)
	})
}

func lastIndexOfType(events []history.Event, typ string) int {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == typ {
			return i
		}
	}
	return -1
}

func getString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func inferBaseCommit(repoDir, taskName, baseBranch string) string {
	taskName = strings.TrimSpace(taskName)
	baseBranch = strings.TrimSpace(baseBranch)
	if taskName == "" || baseBranch == "" {
		return ""
	}

	// Prefer merge-base when the branch exists (this matches "based on base HEAD at creation time").
	if git.BranchExists(repoDir, taskName) && git.BranchExists(repoDir, baseBranch) {
		if mb, err := git.Output(repoDir, "merge-base", taskName, baseBranch); err == nil {
			return strings.TrimSpace(mb)
		}
	}

	// Draft-only tasks may have no branch yet; fall back to base branch HEAD.
	if git.BranchExists(repoDir, baseBranch) {
		if head, err := git.Output(repoDir, "rev-parse", baseBranch); err == nil {
			return strings.TrimSpace(head)
		}
	}

	return ""
}

func inferFrozenStatsForMerge(repoDir, mergedCommit string) (int, int, string) {
	mergedCommit = strings.TrimSpace(mergedCommit)
	if mergedCommit == "" {
		return 0, 0, "cannot compute frozen stats (missing merge commit)"
	}
	if !git.CommitExists(repoDir, mergedCommit) {
		return 0, 0, fmt.Sprintf("cannot compute frozen stats (missing merge commit %s)", mergedCommit)
	}
	parents, err := git.Output(repoDir, "show", "-s", "--format=%P", mergedCommit)
	if err != nil {
		return 0, 0, fmt.Sprintf("cannot compute frozen stats (failed to read parents): %v", err)
	}
	parent := ""
	for _, p := range strings.Fields(parents) {
		parent = strings.TrimSpace(p)
		break
	}
	if parent == "" {
		return 0, 0, "cannot compute frozen stats (no parent commit)"
	}
	if !git.CommitExists(repoDir, parent) {
		return 0, 0, fmt.Sprintf("cannot compute frozen stats (missing parent commit %s)", parent)
	}
	added, removed, err := git.DiffStatRange(repoDir, parent, mergedCommit)
	if err != nil {
		return 0, 0, fmt.Sprintf("cannot compute frozen stats: %v", err)
	}
	return added, removed, ""
}

func inferFrozenStatsForClose(repoDir, baseCommit, branchHead string) (int, int, int, string) {
	baseCommit = strings.TrimSpace(baseCommit)
	branchHead = strings.TrimSpace(branchHead)
	if baseCommit == "" || branchHead == "" {
		return 0, 0, 0, fmt.Sprintf("cannot compute frozen stats (missing base_commit=%t branch_head=%t)", baseCommit == "", branchHead == "")
	}
	if !git.CommitExists(repoDir, baseCommit) {
		return 0, 0, 0, fmt.Sprintf("cannot compute frozen stats (missing base_commit %s)", baseCommit)
	}
	if !git.CommitExists(repoDir, branchHead) {
		return 0, 0, 0, fmt.Sprintf("cannot compute frozen stats (missing branch_head %s)", branchHead)
	}
	added, removed, err := git.DiffStatRange(repoDir, baseCommit, branchHead)
	if err != nil {
		return 0, 0, 0, fmt.Sprintf("cannot compute frozen stats: %v", err)
	}
	commitCount, err := git.RevListCount(repoDir, baseCommit, branchHead)
	if err != nil {
		return 0, 0, 0, fmt.Sprintf("cannot compute commit_count: %v", err)
	}
	return added, removed, commitCount, ""
}
