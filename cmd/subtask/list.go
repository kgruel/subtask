package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/store"
)

// ListCmd implements 'subtask list'.
type ListCmd struct {
	All  bool `short:"a" help:"Include merged and closed tasks (default: open only)"`
	JSON bool `short:"j" help:"Output as JSON"`
}

// Run executes the list command.
func (c *ListCmd) Run() error {
	if _, err := preflightProject(); err != nil {
		return err
	}
	if c.JSON {
		out, err := c.renderJSON()
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	}
	out, err := c.render()
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

// listJSONItem is the JSON schema for a single task in 'subtask list --json'.
type listJSONItem struct {
	Name          string `json:"name"`
	Title         string `json:"title,omitempty"`
	Status        string `json:"status"`
	WorkerStatus  string `json:"worker_status"`
	Stage         string `json:"stage,omitempty"`
	Workspace     string `json:"workspace,omitempty"`
	BaseBranch    string `json:"base_branch,omitempty"`
	FollowUp      string `json:"follow_up,omitempty"`
	Progress      string `json:"progress,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	LastActive    string `json:"last_active,omitempty"`
	ToolCalls     int    `json:"tool_calls"`
	LinesAdded    int    `json:"lines_added"`
	LinesRemoved  int    `json:"lines_removed"`
	ChangesStatus string `json:"changes_status,omitempty"`
	LastRunMS     int    `json:"last_run_ms"`
	LastError     string `json:"last_error,omitempty"`
	HasReview     bool   `json:"has_review,omitempty"`
	TaskDir       string `json:"task_dir,omitempty"`
	HistoryPath   string `json:"history_path,omitempty"`
}

func (c *ListCmd) renderJSON() (string, error) {
	st := store.New()
	data, err := st.List(context.Background(), store.ListOptions{All: c.All})
	if err != nil {
		return "", err
	}

	for _, e := range data.Errors {
		if e.Err == nil {
			continue
		}
		fmt.Fprintf(os.Stderr, "task %s: %v\n", e.Name, e.Err)
	}

	items := make([]listJSONItem, 0, len(data.Tasks))
	for _, it := range data.Tasks {
		item := listJSONItem{
			Name:          it.Name,
			Title:         it.Title,
			Status:        string(it.TaskStatus),
			WorkerStatus:  string(it.WorkerStatus),
			Stage:         it.Stage,
			Workspace:     it.Workspace,
			BaseBranch:    it.BaseBranch,
			FollowUp:      it.FollowUp,
			ToolCalls:     it.ToolCalls,
			LinesAdded:    it.Changes.Added,
			LinesRemoved:  it.Changes.Removed,
			ChangesStatus: string(it.Changes.Status),
			LastRunMS:     it.LastRunDurationMS,
			LastError:     it.LastError,
			TaskDir:       task.Dir(it.Name),
			HistoryPath:   task.HistoryPath(it.Name),
		}
		if it.ProgressTotal > 0 {
			item.Progress = fmt.Sprintf("%d/%d", it.ProgressDone, it.ProgressTotal)
		}
		if !it.StartedAt.IsZero() {
			item.StartedAt = it.StartedAt.UTC().Format(time.RFC3339)
		}
		if !it.LastActive.IsZero() {
			item.LastActive = it.LastActive.UTC().Format(time.RFC3339)
		}
		if rs := task.LoadReviewSummary(it.Name); rs != nil && rs.Count > 0 {
			item.HasReview = true
		}
		items = append(items, item)
	}

	out, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

func (c *ListCmd) render() (string, error) {
	st := store.New()
	data, err := st.List(context.Background(), store.ListOptions{All: c.All})
	if err != nil {
		return "", err
	}

	for _, e := range data.Errors {
		if e.Err == nil {
			continue
		}
		fmt.Fprintf(os.Stderr, "task %s: %v\n", e.Name, e.Err)
	}

	if len(data.Tasks) == 0 && len(data.Workspaces) == 0 {
		return "No tasks.\n", nil
	}

	tasks := make([]TaskInfo, 0, len(data.Tasks))
	for _, it := range data.Tasks {
		info := TaskInfo{
			Name:          it.Name,
			Title:         it.Title,
			FollowUp:      it.FollowUp,
			BaseBranch:    it.BaseBranch,
			TaskStatus:    it.TaskStatus,
			WorkerStatus:  it.WorkerStatus,
			Stage:         it.Stage,
			Workspace:     it.Workspace,
			StartedAt:     it.StartedAt,
			LastActive:    it.LastActive,
			ToolCalls:     it.ToolCalls,
			LinesAdded:    it.Changes.Added,
			LinesRemoved:  it.Changes.Removed,
			ChangesStatus: string(it.Changes.Status),
			LastRunMS:     it.LastRunDurationMS,
			LastError:     it.LastError,
		}
		if it.ProgressTotal > 0 {
			info.Progress = fmt.Sprintf("%d/%d", it.ProgressDone, it.ProgressTotal)
		}
		tasks = append(tasks, info)
	}

	// Mark tasks that have at least one persisted review file.
	for i := range tasks {
		if rs := task.LoadReviewSummary(tasks[i].Name); rs != nil && rs.Count > 0 {
			tasks[i].HasReview = true
		}
	}

	return RenderTaskList(tasks, data.Workspaces), nil
}
