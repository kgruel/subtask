package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
)

type GitMode int

const (
	GitNone GitMode = iota
	GitOpenOnly
	GitAll
	GitTasks
)

type GitPolicy struct {
	Mode GitMode

	// TTL controls how long cached git stats are considered fresh.
	// If zero, a default is used.
	TTL time.Duration

	// Tasks is used when Mode == GitTasks.
	Tasks []string

	IncludeConflicts   bool
	IncludeIntegration bool
}

const defaultGitTTL = 30 * time.Second

func (i *Index) refreshGit(ctx context.Context, p GitPolicy) error {
	if p.Mode == GitNone {
		return nil
	}

	ttl := p.TTL
	if ttl <= 0 {
		ttl = defaultGitTTL
	}
	cutoffNS := i.now().Add(-ttl).UnixNano()

	candidates, err := i.gitCandidates(ctx, p, cutoffNS)
	if err != nil {
		return err
	}
	// Note: integration refresh is not tied to this candidate list; it has separate caching logic.

	type result struct {
		name string

		updateBase      bool
		updateConflicts bool

		linesAdded    *int
		linesRemoved  *int
		commitsBehind *int

		conflictFilesJSON *string

		baseRef   *string
		targetRef *string

		computedAtNS int64
		errMsg       *string
	}

	nowNS := i.now().UnixNano()
	results := make([]result, 0, len(candidates))

	for _, c := range candidates {
		needsBase := !c.computedAtValid || c.computedAtNS < cutoffNS
		needsConflicts := p.IncludeConflicts && (needsBase || !c.conflictsKnown)

		if !needsBase && !needsConflicts {
			continue
		}

		r := result{
			name:            c.name,
			updateBase:      needsBase,
			updateConflicts: needsConflicts,
		}
		if needsBase {
			r.computedAtNS = nowNS
		}

		// Best-effort computations.
		var firstErr error

		repoDir := c.workspace
		if repoDir == "" {
			repoDir = "."
		}

		// Inputs
		var mergeBase string
		if needsBase || needsConflicts {
			if c.workspace != "" && c.baseBranch != "" {
				base, err := git.ResolveDiffBase(c.workspace, "HEAD", c.baseBranch)
				if err == nil {
					mergeBase = base
				} else if firstErr == nil {
					firstErr = err
				}
			}
			if needsBase && mergeBase != "" {
				r.baseRef = &mergeBase
			}
		}

		targetRef := ""
		if c.baseBranch != "" && (needsBase || needsConflicts) {
			// Local-first: use the local base branch ref only.
			targetRef = c.baseBranch
			if !git.BranchExists(repoDir, targetRef) {
				targetRef = ""
				if firstErr == nil {
					firstErr = fmt.Errorf("base branch %q not found", c.baseBranch)
				}
			}
			if needsBase {
				if targetRef != "" {
					r.targetRef = &targetRef
				}
			}
		}

		if needsBase {
			if c.taskStatus == task.TaskStatusMerged {
				tail, err := history.Tail(c.name)
				if err == nil {
					added, removed, ok, err := git.ShowDiffStat(repoDir, tail.LastMergedCommit)
					if err == nil && ok {
						r.linesAdded = &added
						r.linesRemoved = &removed
					} else if err != nil && firstErr == nil {
						firstErr = err
					}
				} else if firstErr == nil {
					firstErr = err
				}
			} else if c.workspace != "" && mergeBase != "" {
				added, removed, err := git.DiffStat(c.workspace, mergeBase)
				if err == nil {
					r.linesAdded = &added
					r.linesRemoved = &removed
				} else if firstErr == nil {
					firstErr = err
				}
			}

			if targetRef != "" {
				// "Behind" means "how many commits the base branch has that the task ref doesn't".
				//
				// Prefer comparing base branch vs the task branch (correct after rebases/merges),
				// and fall back to the pinned base_commit for draft-only tasks where the branch
				// doesn't exist yet.
				baseRef := ""
				if git.BranchExists(repoDir, c.name) {
					baseRef = c.name
				} else {
					baseRef = c.baseCommit
				}

				baseRef = strings.TrimSpace(baseRef)
				if baseRef != "" {
					behind, err := git.CommitsBehind(repoDir, baseRef, targetRef)
					if err == nil {
						r.commitsBehind = &behind
					} else if firstErr == nil {
						firstErr = err
					}
				}
			}
		}

		if needsConflicts {
			conflicts := []string{}
			if c.workspace != "" && targetRef != "" {
				var err error
				conflicts, err = git.MergeConflictFiles(c.workspace, targetRef, "HEAD")
				if err != nil && firstErr == nil {
					firstErr = err
				}
			}
			if b, err := json.Marshal(conflicts); err == nil {
				s := string(b) // "[]" for none
				r.conflictFilesJSON = &s
			}
		}

		if firstErr != nil {
			s := firstErr.Error()
			r.errMsg = &s
		}

		results = append(results, r)
	}

	if len(results) > 0 {
		tx, err := i.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("index git refresh: begin tx: %w", err)
		}
		defer tx.Rollback()

		const q = `
UPDATE tasks SET
	git_lines_added = CASE WHEN ? THEN ? ELSE git_lines_added END,
	git_lines_removed = CASE WHEN ? THEN ? ELSE git_lines_removed END,
	git_commits_behind = CASE WHEN ? THEN ? ELSE git_commits_behind END,
	git_base_ref = CASE WHEN ? THEN ? ELSE git_base_ref END,
	git_target_ref = CASE WHEN ? THEN ? ELSE git_target_ref END,
	git_computed_at_ns = CASE WHEN ? THEN ? ELSE git_computed_at_ns END,
	git_error = CASE WHEN ? THEN ? ELSE git_error END,
	git_conflict_files_json = CASE WHEN ? THEN ? ELSE git_conflict_files_json END
WHERE name = ?;
`
		stmt, err := tx.PrepareContext(ctx, q)
		if err != nil {
			return fmt.Errorf("index git refresh: prepare update: %w", err)
		}
		defer stmt.Close()

		for _, r := range results {
			updateErr := r.updateBase || r.updateConflicts
			if _, err := stmt.ExecContext(ctx,
				boolToInt(r.updateBase),
				nullableInt(r.linesAdded),
				boolToInt(r.updateBase),
				nullableInt(r.linesRemoved),
				boolToInt(r.updateBase),
				nullableInt(r.commitsBehind),
				boolToInt(r.updateBase),
				nullableStringPtr(r.baseRef),
				boolToInt(r.updateBase),
				nullableStringPtr(r.targetRef),
				boolToInt(r.updateBase),
				r.computedAtNS,
				boolToInt(updateErr),
				nullableStringPtr(r.errMsg),
				boolToInt(r.updateConflicts),
				nullableStringPtr(r.conflictFilesJSON),
				r.name,
			); err != nil {
				return fmt.Errorf("index git refresh: update %q: %w", r.name, err)
			}
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("index git refresh: commit: %w", err)
		}
	}

	return i.refreshIntegration(ctx, p)
}

type gitCandidate struct {
	name       string
	baseBranch string
	baseCommit string
	workspace  string

	taskStatus task.TaskStatus

	computedAtNS    int64
	computedAtValid bool
	conflictsKnown  bool
}

func (i *Index) gitCandidates(ctx context.Context, p GitPolicy, cutoffNS int64) ([]gitCandidate, error) {
	staleOrMissing := "(git_computed_at_ns IS NULL OR git_computed_at_ns < ?)"
	if p.IncludeConflicts {
		staleOrMissing += " OR git_conflict_files_json IS NULL"
	}
	// Integration candidates are handled separately.

	switch p.Mode {
	case GitAll:
		return i.gitCandidatesQuery(ctx, fmt.Sprintf(`
SELECT name, base_branch, base_commit, workspace, task_status, git_computed_at_ns, git_conflict_files_json
FROM tasks
WHERE %s;`, staleOrMissing), []any{cutoffNS})
	case GitOpenOnly:
		return i.gitCandidatesQuery(ctx, fmt.Sprintf(`
SELECT name, base_branch, base_commit, workspace, task_status, git_computed_at_ns, git_conflict_files_json
FROM tasks
WHERE task_status != 'closed' AND (%s);`, staleOrMissing), []any{cutoffNS})
	case GitTasks:
		if len(p.Tasks) == 0 {
			return nil, nil
		}
		placeholders := make([]string, 0, len(p.Tasks))
		args := make([]any, 0, len(p.Tasks)+1)
		for _, name := range p.Tasks {
			placeholders = append(placeholders, "?")
			args = append(args, name)
		}
		args = append(args, cutoffNS)
		q := fmt.Sprintf(`
SELECT name, base_branch, base_commit, workspace, task_status, git_computed_at_ns, git_conflict_files_json
FROM tasks
WHERE name IN (%s) AND (%s);`, strings.Join(placeholders, ","), staleOrMissing)
		return i.gitCandidatesQuery(ctx, q, args)
	default:
		return nil, nil
	}
}

func (i *Index) gitCandidatesQuery(ctx context.Context, q string, args []any) ([]gitCandidate, error) {
	rows, err := i.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("index git refresh: query candidates: %w", err)
	}
	defer rows.Close()

	var out []gitCandidate
	for rows.Next() {
		var (
			c          gitCandidate
			taskStatus string
			computedAt sql.NullInt64
			conflicts  sql.NullString
		)
		if err := rows.Scan(&c.name, &c.baseBranch, &c.baseCommit, &c.workspace, &taskStatus, &computedAt, &conflicts); err != nil {
			return nil, fmt.Errorf("index git refresh: scan candidate: %w", err)
		}
		c.taskStatus = task.TaskStatus(taskStatus)
		c.computedAtValid = computedAt.Valid
		c.computedAtNS = computedAt.Int64
		c.conflictsKnown = conflicts.Valid
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("index git refresh: iterate candidates: %w", err)
	}
	return out, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullableStringPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func nullableInt(n *int) any {
	if n == nil {
		return nil
	}
	return *n
}
