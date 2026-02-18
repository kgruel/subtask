package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/task/index"
	"github.com/zippoxer/subtask/pkg/workflow"
	"github.com/zippoxer/subtask/pkg/workspace"
)

type store struct{}

func New() Store {
	return &store{}
}

const defaultListTargetCount = 10

func (s *store) List(ctx context.Context, opts ListOptions) (ListResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	workspaces, err := workspace.ListWorkspaces()
	if err != nil {
		return ListResult{}, err
	}

	taskNames, err := task.List()
	if err != nil {
		return ListResult{}, err
	}
	if len(taskNames) == 0 {
		return ListResult{
			Tasks:               nil,
			Errors:              nil,
			Workspaces:          workspaces,
			AvailableWorkspaces: countAvailableWorkspaces(nil, workspaces),
		}, nil
	}

	idx, err := index.OpenDefault()
	if err != nil {
		return ListResult{}, err
	}
	defer idx.Close()

	if err := idx.Refresh(ctx, index.RefreshPolicy{Git: index.GitPolicy{Mode: index.GitNone}}); err != nil {
		return ListResult{}, err
	}

	targetCount := opts.TargetCount
	if targetCount <= 0 {
		targetCount = defaultListTargetCount
	}

	var items []index.ListItem
	if opts.All {
		ls, err := idx.ListAll(ctx)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, ls...)
	} else {
		open, err := idx.ListOpen(ctx)
		if err != nil {
			return ListResult{}, err
		}
		closed, err := idx.ListClosed(ctx)
		if err != nil {
			return ListResult{}, err
		}

		items = append(items, open...)

		remaining := targetCount - len(open)
		if remaining > 0 {
			if remaining > len(closed) {
				remaining = len(closed)
			}
			items = append(items, closed[:remaining]...)
		}
	}

	available := countAvailableWorkspaces(items, workspaces)

	repoDir := task.ProjectRoot()
	refs, err := git.ListRefs(repoDir, "refs/heads")
	if err != nil {
		return ListResult{}, err
	}

	type changeResult struct {
		name   string
		change Changes
		merged bool
		err    error
	}

	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	results := make(chan changeResult, len(items))

	for _, it := range items {
		if it.TaskStatus != task.TaskStatusOpen {
			continue
		}
		// Draft tasks may not have a branch yet; allow fast path without errors.
		if task.NormalizeWorkerStatus(it.WorkerStatus) == task.WorkerStatusNotStarted {
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(it index.ListItem) {
			defer func() {
				<-sem
				wg.Done()
			}()

			ch, computeErr := computeHistoricalChanges(ctx, idx, repoDir, refs, it.Name, it.BaseBranch, it.BaseCommit, it.Workspace)
			merged := false
			if computeErr == nil && ch.Err == nil && task.NormalizeWorkerStatus(it.WorkerStatus) != task.WorkerStatusRunning {
				// Never auto-merge "empty" tasks at creation time.
				branchHead := resolveHead(refs, it.Name)
				baseHead := resolveHead(refs, it.BaseBranch)
				if branchHead != "" && baseHead != "" && strings.TrimSpace(it.BaseCommit) != "" && branchHead != strings.TrimSpace(it.BaseCommit) {
					isAncestor, err := git.IsAncestor(repoDir, branchHead, baseHead)
					if err != nil {
						results <- changeResult{name: it.Name, change: ch, err: err}
						return
					}
					if isAncestor {
						commitCount, err := git.RevListCount(repoDir, strings.TrimSpace(it.BaseCommit), branchHead)
						if err != nil {
							results <- changeResult{name: it.Name, change: ch, err: err}
							return
						}
						appended, err := appendDetectedMergeEvent(ctx, repoDir, it.Name, it.BaseBranch, strings.TrimSpace(it.BaseCommit), branchHead, baseHead, ch, commitCount)
						if err != nil {
							results <- changeResult{name: it.Name, change: ch, err: err}
							return
						}
						merged = appended
					}
				}
			}

			// Content detection: if merging the task branch into base would add nothing, the work is already in base.
			// This is an informational UX signal (does not change durable task status).
			if !merged && computeErr == nil && ch.Err == nil && task.NormalizeWorkerStatus(it.WorkerStatus) != task.WorkerStatusRunning {
				branchHead := resolveHead(refs, it.Name)
				baseHead := resolveHead(refs, it.BaseBranch)
				if branchHead != "" && baseHead != "" && git.CommitExists(repoDir, branchHead) && git.CommitExists(repoDir, baseHead) {
					reason, _ := integrationReason(ctx, idx, repoDir, it.Name, branchHead, baseHead)
					if reason != "" && reason != git.IntegratedAncestor && reason != git.IntegratedSameCommit {
						mb, err := git.MergeBase(repoDir, branchHead, baseHead)
						if err == nil && strings.TrimSpace(mb) != "" {
							out, err := git.Output(repoDir, "diff", "--name-only", strings.TrimSpace(mb)+".."+branchHead)
							if err == nil && strings.TrimSpace(out) != "" {
								ch.Status = ChangesStatusApplied
							}
						}
					}
				}
			}

			results <- changeResult{name: it.Name, change: ch, merged: merged, err: computeErr}
		}(it)
	}

	wg.Wait()
	close(results)

	changeByName := make(map[string]Changes, len(items))
	mergedByName := make(map[string]bool, len(items))
	var errs []TaskLoadError
	for r := range results {
		changeByName[r.name] = r.change
		if r.merged {
			mergedByName[r.name] = true
		}
		if r.err != nil {
			errs = append(errs, TaskLoadError{Name: r.name, Err: r.err})
		}
	}

	out := ListResult{
		Tasks:               make([]TaskListItem, 0, len(items)),
		Errors:              errs,
		Workspaces:          workspaces,
		AvailableWorkspaces: available,
	}

	for _, it := range items {
		taskItem := TaskListItem{
			Name:              it.Name,
			Title:             it.Title,
			FollowUp:          it.FollowUp,
			BaseBranch:        it.BaseBranch,
			BaseCommit:        it.BaseCommit,
			TaskStatus:        it.TaskStatus,
			WorkerStatus:      it.WorkerStatus,
			Stage:             it.Stage,
			Workspace:         it.Workspace,
			StartedAt:         it.StartedAt,
			LastActive:        it.LastActive,
			ToolCalls:         it.ToolCalls,
			ProgressDone:      it.ProgressDone,
			ProgressTotal:     it.ProgressTotal,
			LastRunDurationMS: it.LastRunDurationMS,
			LastError:         it.LastError,
			Changes: Changes{
				Added:   it.LinesAdded,
				Removed: it.LinesRemoved,
			},
		}

		if mergedByName[it.Name] {
			taskItem.TaskStatus = task.TaskStatusMerged
		}

		if it.TaskStatus == task.TaskStatusOpen {
			if task.NormalizeWorkerStatus(it.WorkerStatus) == task.WorkerStatusNotStarted {
				// Keep draft tasks fast and clean: no branch required, no errors.
				taskItem.Changes = Changes{}
			} else if ch, ok := changeByName[it.Name]; ok {
				taskItem.Changes = ch
			}
		}

		out.Tasks = append(out.Tasks, taskItem)
	}

	return out, nil
}

func (s *store) Get(ctx context.Context, name string, _ GetOptions) (TaskView, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	idx, err := index.OpenDefault()
	if err != nil {
		return TaskView{}, err
	}
	defer idx.Close()

	if err := idx.Refresh(ctx, index.RefreshPolicy{Git: index.GitPolicy{Mode: index.GitNone}}); err != nil {
		return TaskView{}, err
	}

	rec, ok, err := idx.Get(ctx, name)
	if err != nil {
		return TaskView{}, err
	}
	if !ok || rec.Task == nil {
		// Preserve historical errors for missing/invalid tasks.
		_, err := task.Load(name)
		return TaskView{}, err
	}

	t := rec.Task
	state := rec.State
	meta := rec.ProgressMeta
	cfg, _ := workspace.LoadConfig() // best-effort (allows working in partial setups)

	view := TaskView{
		Task:          t,
		BaseCommit:    rec.BaseCommit,
		State:         state,
		ProgressMeta:  meta,
		ProgressSteps: task.LoadProgressSteps(name),
		Model:         workspace.ResolveModel(cfg, t, ""),
		TaskStatus:    rec.TaskStatus,
		WorkerStatus:  rec.WorkerStatus,
		Stage:         rec.Stage,
		LastHistoryNS: rec.LastHistory.UnixNano(),
		LastRunMS:     rec.LastRunDurationMS,
	}
	if cfg != nil && cfg.Adapter == "codex" {
		view.Reasoning = workspace.ResolveReasoning(cfg, t, "")
	}

	// Workflow for this task, if any.
	if wf, err := workflow.LoadFromTask(name); err == nil {
		view.Workflow = wf
	}

	// Task folder files.
	taskDir := task.Dir(name)
	entries, err := os.ReadDir(taskDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				view.TaskFiles = append(view.TaskFiles, e.Name())
			}
		}
	}

	repoDir := task.ProjectRoot()
	refs, err := git.ListRefs(repoDir, "refs/heads")
	if err != nil {
		return TaskView{}, err
	}

	// Historical changes + commit count (detail-only) for open tasks.
	if rec.TaskStatus == task.TaskStatusOpen {
		ws := task.NormalizeWorkerStatus(rec.WorkerStatus)
		if ws == task.WorkerStatusNotStarted {
			view.Changes = Changes{}
			view.Commits = Commits{}
		} else {
			workspacePath := ""
			if state != nil {
				workspacePath = state.Workspace
			}
			view.Changes, _ = computeHistoricalChanges(ctx, idx, repoDir, refs, name, t.BaseBranch, rec.BaseCommit, workspacePath)
			view.Commits, _ = computeCommitCount(ctx, idx, repoDir, refs, name, t.BaseBranch, rec.BaseCommit, workspacePath)

			// External merge detection (ancestor-only): if branch tip is in base, record a durable merge event.
			branchHead := resolveHead(refs, name)
			baseHead := resolveHead(refs, t.BaseBranch)
			if ws != task.WorkerStatusRunning && branchHead != "" && baseHead != "" && strings.TrimSpace(rec.BaseCommit) != "" && branchHead != strings.TrimSpace(rec.BaseCommit) {
				isAncestor, err := git.IsAncestor(repoDir, branchHead, baseHead)
				if err == nil && isAncestor && view.Changes.Err == nil && view.Commits.Err == nil {
					appended, err := appendDetectedMergeEvent(ctx, repoDir, name, t.BaseBranch, strings.TrimSpace(rec.BaseCommit), branchHead, baseHead, view.Changes, view.Commits.Count)
					if err == nil && appended {
						view.TaskStatus = task.TaskStatusMerged
					}
				}
			}

			// Content detection: show "applied" when branch content is already in base but history is not merged.
			if view.TaskStatus == task.TaskStatusOpen && ws != task.WorkerStatusRunning && branchHead != "" && baseHead != "" && git.CommitExists(repoDir, branchHead) && git.CommitExists(repoDir, baseHead) {
				reason := git.IntegrationReason("")
				if strings.TrimSpace(rec.IntegratedBranchHead) == branchHead && strings.TrimSpace(rec.IntegratedTargetHead) == baseHead {
					reason = git.IntegrationReason(strings.TrimSpace(rec.IntegratedReason))
				} else {
					reason = git.IsIntegrated(repoDir, branchHead, baseHead)
					_ = idx.UpdateIntegrationCache(ctx, name, branchHead, baseHead, string(reason))
				}

				if reason != "" && reason != git.IntegratedAncestor && reason != git.IntegratedSameCommit {
					mb, err := git.MergeBase(repoDir, branchHead, baseHead)
					if err == nil && strings.TrimSpace(mb) != "" {
						out, err := git.Output(repoDir, "diff", "--name-only", strings.TrimSpace(mb)+".."+branchHead)
						if err == nil && strings.TrimSpace(out) != "" {
							view.Changes.Status = ChangesStatusApplied
						}
					}
				}
			}
		}
	} else {
		// Back-compat: keep the existing cached counts until frozen stats land.
		view.Changes = Changes{Added: rec.LinesAdded, Removed: rec.LinesRemoved}
	}

	// Conflicts: best-effort, computed on demand.
	if rec.TaskStatus == task.TaskStatusOpen && state != nil && state.Workspace != "" && strings.TrimSpace(t.BaseBranch) != "" {
		conflicts, err := git.MergeConflictFiles(state.Workspace, t.BaseBranch, "HEAD")
		if err == nil && len(conflicts) > 0 {
			view.ConflictFiles = conflicts
		}
	} else if rec.ConflictFilesJSON != "" {
		// Back-compat for any cached conflict list.
		var conflicts []string
		if err := json.Unmarshal([]byte(rec.ConflictFilesJSON), &conflicts); err == nil && len(conflicts) > 0 {
			view.ConflictFiles = conflicts
		}
	}

	return view, nil
}

func appendDetectedMergeEvent(ctx context.Context, repoDir string, taskName string, baseBranch string, baseCommit string, branchHead string, baseHead string, ch Changes, commitCount int) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	taskName = strings.TrimSpace(taskName)
	baseBranch = strings.TrimSpace(baseBranch)
	baseCommit = strings.TrimSpace(baseCommit)
	branchHead = strings.TrimSpace(branchHead)
	baseHead = strings.TrimSpace(baseHead)

	if taskName == "" || baseBranch == "" || baseCommit == "" || branchHead == "" || baseHead == "" {
		return false, nil
	}

	// Safety rail: never auto-merge empty branches at creation time.
	if branchHead == baseCommit {
		return false, nil
	}

	appended := false
	err := task.WithLock(taskName, func() error {
		tail, err := history.Tail(taskName)
		if err != nil {
			return err
		}
		if tail.TaskStatus != task.TaskStatusOpen {
			return nil
		}

		// Re-check heads while locked to avoid racing concurrent writes.
		currentBranchHead, err := git.Output(repoDir, "rev-parse", taskName)
		if err != nil {
			return err
		}
		currentBranchHead = strings.TrimSpace(currentBranchHead)
		if currentBranchHead == "" || currentBranchHead != branchHead {
			return nil
		}

		currentBaseHead, err := git.Output(repoDir, "rev-parse", baseBranch)
		if err != nil {
			return err
		}
		currentBaseHead = strings.TrimSpace(currentBaseHead)
		if currentBaseHead == "" {
			return nil
		}

		isAncestor, err := git.IsAncestor(repoDir, currentBranchHead, currentBaseHead)
		if err != nil {
			return err
		}
		if !isAncestor {
			return nil
		}

		data, err := json.Marshal(map[string]any{
			// Back-compat
			"commit": strings.TrimSpace(currentBranchHead),
			"into":   baseBranch,
			"branch": taskName,

			// Redesign fields
			"via":             "detected",
			"method":          "ancestor",
			"base_branch":     baseBranch,
			"base_commit":     baseCommit,
			"branch_head":     strings.TrimSpace(currentBranchHead),
			"base_head":       strings.TrimSpace(currentBaseHead),
			"target_head":     strings.TrimSpace(currentBaseHead),
			"changes_added":   ch.Added,
			"changes_removed": ch.Removed,
			"commit_count":    commitCount,
			"detected_at":     time.Now().UTC().Unix(),
		})
		if err != nil {
			return err
		}

		if err := history.AppendLocked(taskName, history.Event{Type: "task.merged", Data: data}); err != nil {
			return err
		}
		appended = true
		return nil
	})
	return appended, err
}

func appendCommitEvents(ctx context.Context, idx *index.Index, repoDir string, taskName string, baseCommit string, branchHead string, lastLoggedHead string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	taskName = strings.TrimSpace(taskName)
	baseCommit = strings.TrimSpace(baseCommit)
	branchHead = strings.TrimSpace(branchHead)
	lastLoggedHead = strings.TrimSpace(lastLoggedHead)

	if taskName == "" || baseCommit == "" || branchHead == "" {
		return nil
	}
	if lastLoggedHead == branchHead {
		return nil
	}

	// For empty branches (no commits since base_commit), we still want to mark the head as scanned
	// so we don't re-run commit logging on every list/show call.
	var commits []git.CommitMeta
	if branchHead != baseCommit {
		from := baseCommit
		if lastLoggedHead != "" {
			if isAncestor, err := git.IsAncestor(repoDir, lastLoggedHead, branchHead); err == nil && isAncestor {
				from = lastLoggedHead
			}
		}
		var err error
		commits, err = git.ListCommitsRange(repoDir, from, branchHead)
		if err != nil {
			return err
		}
	}

	seenAt := time.Now().UTC().Unix()
	return task.WithLock(taskName, func() error {
		tail, err := history.Tail(taskName)
		if err != nil {
			return err
		}
		if tail.TaskStatus != task.TaskStatusOpen {
			return nil
		}

		// Ensure the branch head is still the same before we write history.
		currentBranchHead, err := git.Output(repoDir, "rev-parse", taskName)
		if err != nil {
			return err
		}
		currentBranchHead = strings.TrimSpace(currentBranchHead)
		if currentBranchHead == "" || currentBranchHead != branchHead {
			return nil
		}

		// Dedupe by SHA from existing history.
		evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
		if err != nil {
			return err
		}
		seen := make(map[string]struct{}, len(evs))
		for _, ev := range evs {
			if ev.Type != "task.commit" {
				continue
			}
			var d struct {
				SHA string `json:"sha"`
			}
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				continue
			}
			sha := strings.TrimSpace(d.SHA)
			if sha != "" {
				seen[sha] = struct{}{}
			}
		}

		for _, c := range commits {
			sha := strings.TrimSpace(c.SHA)
			if sha == "" {
				continue
			}
			if _, ok := seen[sha]; ok {
				continue
			}

			data, err := json.Marshal(map[string]any{
				"sha":          sha,
				"subject":      c.Subject,
				"author_name":  c.AuthorName,
				"author_email": c.AuthorEmail,
				"authored_at":  c.AuthoredAt,
				"seen_at":      seenAt,
			})
			if err != nil {
				return err
			}
			if err := history.AppendLocked(taskName, history.Event{Type: "task.commit", Data: data}); err != nil {
				return err
			}
			seen[sha] = struct{}{}
		}

		return idx.UpdateCommitLogLastHead(ctx, taskName, branchHead)
	})
}

func countAvailableWorkspaces(items []index.ListItem, workspaces []workspace.Entry) int {
	used := make(map[string]bool, len(items))
	for _, it := range items {
		if it.Workspace != "" {
			used[it.Workspace] = true
		}
	}
	available := 0
	for _, ws := range workspaces {
		if !used[ws.Path] {
			available++
		}
	}
	return available
}

func resolveHead(refs map[string]string, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return strings.TrimSpace(refs["refs/heads/"+name])
}

func resolveWorkspaceHead(workspacePath string) string {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return ""
	}
	if st, err := os.Stat(workspacePath); err != nil || !st.IsDir() {
		return ""
	}
	head, err := git.Output(workspacePath, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(head)
}

func integrationReason(ctx context.Context, idx *index.Index, repoDir string, taskName string, branchHead string, baseHead string) (git.IntegrationReason, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	taskName = strings.TrimSpace(taskName)
	branchHead = strings.TrimSpace(branchHead)
	baseHead = strings.TrimSpace(baseHead)
	if taskName == "" || branchHead == "" || baseHead == "" {
		return "", nil
	}

	if idx != nil {
		rec, ok, err := idx.Get(ctx, taskName)
		if err == nil && ok {
			if strings.TrimSpace(rec.IntegratedBranchHead) == branchHead && strings.TrimSpace(rec.IntegratedTargetHead) == baseHead {
				return git.IntegrationReason(strings.TrimSpace(rec.IntegratedReason)), nil
			}
		}
	}

	reason := git.IsIntegrated(repoDir, branchHead, baseHead)
	if idx != nil {
		_ = idx.UpdateIntegrationCache(ctx, taskName, branchHead, baseHead, string(reason))
	}
	return reason, nil
}

func computeHistoricalChanges(ctx context.Context, idx *index.Index, repoDir string, refs map[string]string, taskName string, baseBranch string, baseCommit string, workspacePath string) (Changes, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	taskName = strings.TrimSpace(taskName)
	baseBranch = strings.TrimSpace(baseBranch)
	taskBaseCommit := strings.TrimSpace(baseCommit)

	branchRefHead := resolveHead(refs, taskName)
	branchHead := branchRefHead
	if branchHead == "" {
		branchHead = resolveWorkspaceHead(workspacePath)
	}
	baseHead := resolveHead(refs, baseBranch)

	rec, ok, err := idx.Get(ctx, taskName)
	if err != nil {
		return Changes{Err: err}, err
	}
	if ok {
		// Best-effort: keep these around for debugging.
		if strings.TrimSpace(rec.BranchHead) != branchHead || strings.TrimSpace(rec.BaseHead) != baseHead {
			_ = idx.UpdateRefHeads(ctx, taskName, branchHead, baseHead)
		}
	}

	if branchHead == "" {
		miss := fmt.Errorf("%w: %s", ErrBranchMissing, taskName)
		if ok && strings.TrimSpace(rec.BranchHead) != "" {
			miss = fmt.Errorf("%w: %s", ErrBranchDeleted, taskName)
		}
		return Changes{Status: ChangesStatusMissing, Err: miss}, nil
	}
	if !git.CommitExists(repoDir, branchHead) {
		miss := fmt.Errorf("%w: branch_head %s", ErrCommitMissing, branchHead)
		return Changes{Status: ChangesStatusMissing, Err: miss}, nil
	}

	mergeBase := ""
	if strings.TrimSpace(baseBranch) != "" {
		if mb, err := git.MergeBase(repoDir, branchHead, baseBranch); err == nil {
			mergeBase = strings.TrimSpace(mb)
		}
	}
	if mergeBase == "" {
		return Changes{Err: fmt.Errorf("cannot compute merge-base for %s (base_branch=%q)", taskName, baseBranch)}, fmt.Errorf("cannot compute merge-base for %s (base_branch=%q)", taskName, baseBranch)
	}
	if !git.CommitExists(repoDir, mergeBase) {
		miss := fmt.Errorf("%w: merge_base %s", ErrCommitMissing, mergeBase)
		return Changes{Status: ChangesStatusMissing, Err: miss}, nil
	}

	// Diff base: use merge-base for open tasks (GitHub PR semantics), but if the branch tip is already
	// reachable from base (fast-forward merged), merge-base collapses to the branch tip and stats become 0/0.
	// In that case, try fork-point (uses base reflog) and fall back to the task's stored base_commit.
	diffBase := mergeBase
	if strings.TrimSpace(diffBase) != "" && strings.TrimSpace(branchHead) != "" && strings.TrimSpace(diffBase) == strings.TrimSpace(branchHead) {
		if fp, err := git.MergeBaseForkPoint(repoDir, baseBranch, branchHead); err == nil {
			fp = strings.TrimSpace(fp)
			if fp != "" && fp != branchHead && git.CommitExists(repoDir, fp) {
				diffBase = fp
			}
		}
		if strings.TrimSpace(diffBase) == strings.TrimSpace(branchHead) && taskBaseCommit != "" && taskBaseCommit != branchHead && git.CommitExists(repoDir, taskBaseCommit) {
			diffBase = taskBaseCommit
		}
	}

	// Best-effort: log commit events when the branch head advances.
	// This is a side-effect, but computeHistoricalChanges is called on every store read for open tasks.
	if ok && branchRefHead != "" {
		_ = appendCommitEvents(ctx, idx, repoDir, taskName, diffBase, branchRefHead, rec.CommitLogLastHead)
	}

	// Cache hit.
	if ok && rec.ChangesBaseCommit == diffBase && rec.ChangesBranchHead == branchHead {
		return Changes{Added: rec.ChangesAdded, Removed: rec.ChangesRemoved}, nil
	}

	added, removed, err := git.DiffStatRange(repoDir, diffBase, branchHead)
	if err != nil {
		return Changes{Err: err}, err
	}
	_ = idx.UpdateChangesCache(ctx, taskName, diffBase, branchHead, added, removed)
	return Changes{Added: added, Removed: removed}, nil
}

func computeCommitCount(ctx context.Context, idx *index.Index, repoDir string, refs map[string]string, taskName string, baseBranch string, baseCommit string, workspacePath string) (Commits, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	taskName = strings.TrimSpace(taskName)
	baseBranch = strings.TrimSpace(baseBranch)
	taskBaseCommit := strings.TrimSpace(baseCommit)

	branchHead := resolveHead(refs, taskName)
	if branchHead == "" {
		branchHead = resolveWorkspaceHead(workspacePath)
	}
	baseHead := resolveHead(refs, baseBranch)

	rec, ok, err := idx.Get(ctx, taskName)
	if err != nil {
		return Commits{Err: err}, err
	}
	if ok {
		if strings.TrimSpace(rec.BranchHead) != branchHead || strings.TrimSpace(rec.BaseHead) != baseHead {
			_ = idx.UpdateRefHeads(ctx, taskName, branchHead, baseHead)
		}
	}

	if branchHead == "" {
		miss := fmt.Errorf("%w: %s", ErrBranchMissing, taskName)
		if ok && strings.TrimSpace(rec.BranchHead) != "" {
			miss = fmt.Errorf("%w: %s", ErrBranchDeleted, taskName)
		}
		return Commits{Err: miss}, miss
	}
	if !git.CommitExists(repoDir, branchHead) {
		return Commits{Err: fmt.Errorf("%w: branch_head %s", ErrCommitMissing, branchHead)}, fmt.Errorf("%w: branch_head %s", ErrCommitMissing, branchHead)
	}

	mergeBase := ""
	if strings.TrimSpace(baseBranch) != "" {
		if mb, err := git.MergeBase(repoDir, branchHead, baseBranch); err == nil {
			mergeBase = strings.TrimSpace(mb)
		}
	}
	if mergeBase == "" {
		return Commits{Err: fmt.Errorf("cannot compute merge-base for %s (base_branch=%q)", taskName, baseBranch)}, fmt.Errorf("cannot compute merge-base for %s (base_branch=%q)", taskName, baseBranch)
	}
	if !git.CommitExists(repoDir, mergeBase) {
		return Commits{Err: fmt.Errorf("%w: merge_base %s", ErrCommitMissing, mergeBase)}, fmt.Errorf("%w: merge_base %s", ErrCommitMissing, mergeBase)
	}

	diffBase := mergeBase
	if strings.TrimSpace(diffBase) != "" && strings.TrimSpace(branchHead) != "" && strings.TrimSpace(diffBase) == strings.TrimSpace(branchHead) {
		if fp, err := git.MergeBaseForkPoint(repoDir, baseBranch, branchHead); err == nil {
			fp = strings.TrimSpace(fp)
			if fp != "" && fp != branchHead && git.CommitExists(repoDir, fp) {
				diffBase = fp
			}
		}
		if strings.TrimSpace(diffBase) == strings.TrimSpace(branchHead) && taskBaseCommit != "" && taskBaseCommit != branchHead && git.CommitExists(repoDir, taskBaseCommit) {
			diffBase = taskBaseCommit
		}
	}

	if ok && rec.CommitCountBaseCommit == diffBase && rec.CommitCountBranchHead == branchHead {
		return Commits{Count: rec.CommitCount}, nil
	}

	n, err := git.RevListCount(repoDir, diffBase, branchHead)
	if err != nil {
		return Commits{Err: err}, err
	}
	_ = idx.UpdateCommitCountCache(ctx, taskName, diffBase, branchHead, n)
	return Commits{Count: n}, nil
}
