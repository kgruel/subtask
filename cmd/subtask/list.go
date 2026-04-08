package main

import (
	"context"
	"fmt"
	"os"

	"github.com/kgruel/subtask/pkg/task/store"
)

// ListCmd implements 'subtask list'.
type ListCmd struct {
	All bool `short:"a" help:"Show all tasks including closed"`
}

// Run executes the list command.
func (c *ListCmd) Run() error {
	if _, err := preflightProject(); err != nil {
		return err
	}
	out, err := c.render()
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
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

	return RenderTaskList(tasks, data.Workspaces), nil
}
