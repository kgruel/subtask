package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/render"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/store"
)

// ShowCmd implements 'subtask show'.
type ShowCmd struct {
	Task    string `arg:"" help:"Task name to show"`
	Watch   bool   `short:"w" help:"Refresh every 2s (TTY only)"`
	JSON    bool   `short:"j" help:"Output as JSON"`
	Verbose bool   `short:"v" help:"Show all fields (workspace, directory, base commit)"`
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
	view, err := store.BuildView(context.Background(), c.Task)
	if err != nil {
		return "", err
	}

	card := render.TaskCardFromView(view, c.Verbose)

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
	RoutineSource   string                 `json:"routine_source,omitempty"`
	Agent           string                 `json:"agent,omitempty"`
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

	if rs := task.LoadReviewSummary(c.Task); rs != nil && rs.Count > 0 {
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
		out.RoutineSource = detail.Routine.Source
		if strings.TrimSpace(detail.Stage) != "" {
			if step := detail.Routine.GetStep(detail.Stage); step != nil && step.Agent != "" {
				out.Agent = step.Agent
			}
		}
	}
	if out.Agent == "" && detail.Task.Agent != "" {
		out.Agent = detail.Task.Agent
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
