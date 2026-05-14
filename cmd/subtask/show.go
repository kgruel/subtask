package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/render"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/store"
)

// ShowCmd implements 'subtask show'.
type ShowCmd struct {
	Task  string `arg:"" help:"Task name to show"`
	Watch bool   `short:"w" help:"Refresh every 2s (TTY only)"`
	JSON  bool   `short:"j" help:"Output as JSON"`
}

// Run executes the show command.
func (c *ShowCmd) Run() error {
	if _, err := preflightProject(); err != nil {
		return err
	}
	if c.JSON {
		if c.Watch {
			return fmt.Errorf("--watch cannot be used with --json")
		}
		out, err := c.renderJSON()
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	}

	if c.Watch {
		return runWatch(c.render)
	}

	out, err := c.render()
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

func (c *ShowCmd) render() (string, error) {
	st := store.New()
	detail, err := st.Get(context.Background(), c.Task, store.GetOptions{})
	if err != nil {
		return "", err
	}

	t := detail.Task
	state := detail.State

	// Build task card.
	card := &render.TaskCard{
		Name:       t.Name,
		Title:      t.Title,
		Branch:     t.Name,
		BaseBranch: t.BaseBranch,
		BaseCommit: detail.BaseCommit,
	}
	card.Model = detail.Model
	card.Reasoning = detail.Reasoning

	lastError := ""
	if state != nil {
		lastError = state.LastError
	}
	card.TaskStatus = userStatusText(detail.TaskStatus, detail.WorkerStatus, time.Time{}, detail.LastRunMS, lastError)
	if state != nil && detail.WorkerStatus == task.WorkerStatusRunning && !state.StartedAt.IsZero() {
		card.TaskStatus = userStatusText(detail.TaskStatus, detail.WorkerStatus, state.StartedAt, detail.LastRunMS, lastError)
	}

	if state != nil {
		card.Workspace = state.Workspace
		if detail.WorkerStatus == task.WorkerStatusError && strings.TrimSpace(state.LastError) != "" {
			card.Error = state.LastError
		}
	}

	card.LinesAdded = detail.Changes.Added
	card.LinesRemoved = detail.Changes.Removed
	card.ChangesStatus = string(detail.Changes.Status)
	if detail.Changes.Err != nil && detail.Changes.Status != store.ChangesStatusMissing {
		card.ChangesError = detail.Changes.Err.Error()
	}
	card.CommitCount = detail.Commits.Count
	if detail.Commits.Err != nil {
		card.CommitError = detail.Commits.Err.Error()
	}
	card.ShowCommits = detail.TaskStatus == task.TaskStatusOpen
	card.ConflictFiles = detail.ConflictFiles

	if rs := loadReviewSummary(c.Task); rs.Count > 0 {
		card.ReviewCount = rs.Count
		card.LastReviewTS = rs.LastTS
		card.LastReviewKind = rs.LastKind
		card.LastReviewer = rs.LastAdapter
	}

	if detail.Routine != nil {
		card.Routine = detail.Routine.Name
		if strings.TrimSpace(detail.Stage) != "" {
			card.Stage = render.FormatStageProgression(detail.Routine.StepIDs(), detail.Stage)
		}
	}

	// Load progress steps.
	card.ProgressSteps = make([]render.ProgressStep, 0, len(detail.ProgressSteps))
	for _, s := range detail.ProgressSteps {
		card.ProgressSteps = append(card.ProgressSteps, render.ProgressStep{Step: s.Step, Done: s.Done})
	}
	if len(card.ProgressSteps) > 0 {
		card.Progress = "" // Don't show summary when we have steps.
	}

	card.TaskDir = task.Dir(c.Task)
	card.Files = detail.TaskFiles

	if render.Pretty {
		return card.RenderPretty(), nil
	}
	return card.RenderPlain(), nil
}

type showJSONProgressStep struct {
	Step string `json:"step"`
	Done bool   `json:"done"`
}

type showJSON struct {
	Name            string                 `json:"name"`
	Title           string                 `json:"title,omitempty"`
	Branch          string                 `json:"branch,omitempty"`
	BaseBranch      string                 `json:"base_branch,omitempty"`
	BaseCommit      string                 `json:"base_commit,omitempty"`
	Model           string                 `json:"model,omitempty"`
	Reasoning       string                 `json:"reasoning,omitempty"`
	Status          string                 `json:"status,omitempty"`
	WorkerStatus    string                 `json:"worker_status,omitempty"`
	Error           string                 `json:"error,omitempty"`
	Workspace       string                 `json:"workspace,omitempty"`
	Routine         string                 `json:"routine,omitempty"`
	Stage           string                 `json:"stage,omitempty"`
	TaskDir         string                 `json:"task_dir,omitempty"`
	Files           []string               `json:"files,omitempty"`
	ProgressSteps   []showJSONProgressStep `json:"progress_steps,omitempty"`
	LinesAdded      int                    `json:"lines_added,omitempty"`
	LinesRemoved    int                    `json:"lines_removed,omitempty"`
	CommitCount     int                    `json:"commit_count,omitempty"`
	ConflictFiles   []string               `json:"conflict_files,omitempty"`
	ReviewCount     int                    `json:"review_count,omitempty"`
	LastReviewAt    string                 `json:"last_review_at,omitempty"`
	HistoryPath     string                 `json:"history_path,omitempty"`
	LastWorkerReply string                 `json:"last_worker_reply,omitempty"`
}

func (c *ShowCmd) renderJSON() (string, error) {
	st := store.New()
	detail, err := st.Get(context.Background(), c.Task, store.GetOptions{})
	if err != nil {
		return "", err
	}

	t := detail.Task
	state := detail.State

	out := showJSON{
		Name:            t.Name,
		Title:           t.Title,
		Branch:          t.Name,
		BaseBranch:      t.BaseBranch,
		BaseCommit:      detail.BaseCommit,
		Model:           detail.Model,
		Reasoning:       detail.Reasoning,
		HistoryPath:     task.HistoryPath(c.Task),
		LastWorkerReply: lastWorkerReply(c.Task),
		TaskDir:         task.Dir(c.Task),
		Files:           detail.TaskFiles,
		LinesAdded:      detail.Changes.Added,
		LinesRemoved:    detail.Changes.Removed,
		CommitCount:     detail.Commits.Count,
		ConflictFiles:   detail.ConflictFiles,
	}

	if rs := loadReviewSummary(c.Task); rs.Count > 0 {
		out.ReviewCount = rs.Count
		if !rs.LastTS.IsZero() {
			out.LastReviewAt = rs.LastTS.UTC().Format(time.RFC3339)
		}
	}

	out.Status = string(detail.TaskStatus)
	out.WorkerStatus = string(detail.WorkerStatus)
	out.Stage = detail.Stage
	if state != nil {
		out.Workspace = state.Workspace
		if detail.WorkerStatus == task.WorkerStatusError && state.LastError != "" {
			out.Error = state.LastError
		}
	}

	if detail.Routine != nil {
		out.Routine = detail.Routine.Name
	}

	if steps := detail.ProgressSteps; len(steps) > 0 {
		out.ProgressSteps = make([]showJSONProgressStep, len(steps))
		for i, s := range steps {
			out.ProgressSteps[i] = showJSONProgressStep{Step: s.Step, Done: s.Done}
		}
	}

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

// reviewSummary holds aggregated review file metadata for display.
type reviewSummary struct {
	Count       int
	LastTS      time.Time
	LastKind    string
	LastAdapter string
}

// loadReviewSummary scans the task's reviews/ directory and returns aggregate info.
// Files are sorted alphabetically; since names begin with a compact ISO timestamp
// the last entry is the most recent review.
func loadReviewSummary(taskName string) reviewSummary {
	dir := task.ReviewsDir(taskName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return reviewSummary{}
	}

	// Only count files matching the generated review filename format:
	//   <timestamp>-<runID>-<kind>-<adapter>.md
	// Stray files (a manual README, a legacy filename) are ignored — Count
	// reflects reviews this command produced, not arbitrary .md in the dir.
	type parsed struct {
		name    string
		ts      time.Time
		kind    string
		adapter string
	}
	var matched []parsed
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".md")
		// SplitN with n=4 captures adapter (may contain hyphens) as the final part.
		parts := strings.SplitN(stem, "-", 4)
		if len(parts) != 4 {
			continue
		}
		ts, err := time.Parse("20060102T150405Z", parts[0])
		if err != nil {
			continue
		}
		matched = append(matched, parsed{name: e.Name(), ts: ts, kind: parts[2], adapter: parts[3]})
	}
	if len(matched) == 0 {
		return reviewSummary{}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].name < matched[j].name })
	last := matched[len(matched)-1]
	return reviewSummary{
		Count:       len(matched),
		LastTS:      last.ts,
		LastKind:    last.kind,
		LastAdapter: last.adapter,
	}
}

func lastWorkerReply(taskName string) string {
	events, err := history.Read(taskName, history.ReadOptions{MessagesOnly: true})
	if err != nil || len(events) == 0 {
		return ""
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type != "message" || strings.TrimSpace(events[i].Role) != "worker" {
			continue
		}
		return strings.TrimSpace(events[i].Content)
	}
	return ""
}
