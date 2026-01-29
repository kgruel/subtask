package gather

import (
	"context"
	"fmt"
	"time"

	"github.com/zippoxer/subtask/pkg/logging"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/index"
	"github.com/zippoxer/subtask/pkg/workspace"
)

// DefaultListTargetCount is the minimum number of tasks shown by default.
// Closed tasks are used to fill up to this number (unless All is set).
const DefaultListTargetCount = 10

type ListOptions struct {
	All bool
	// TargetCount only applies when All is false. If zero, DefaultListTargetCount is used.
	TargetCount int
}

type TaskListItem = index.ListItem

type TaskListData struct {
	Items               []TaskListItem
	Workspaces          []workspace.Entry
	AvailableWorkspaces int
}

func List(ctx context.Context, opts ListOptions) (TaskListData, error) {
	debug := logging.DebugEnabled()
	var start time.Time
	if debug {
		start = time.Now()
		logging.Debug("refresh", fmt.Sprintf("gather.List opts={All:%t TargetCount:%d}", opts.All, opts.TargetCount))
	}

	workspaces, err := workspace.ListWorkspaces()
	if err != nil {
		logging.Error("refresh", "workspace.ListWorkspaces error: "+err.Error())
		return TaskListData{}, err
	}
	if debug {
		logging.Debug("refresh", fmt.Sprintf("workspace.ListWorkspaces ok n=%d (+%s)", len(workspaces), time.Since(start).Round(time.Millisecond)))
	}

	taskNames, err := task.List()
	if err != nil {
		logging.Error("refresh", "task.List error: "+err.Error())
		return TaskListData{}, err
	}
	if len(taskNames) == 0 {
		available := countAvailableWorkspaces(nil, workspaces)
		return TaskListData{
			Items:               nil,
			Workspaces:          workspaces,
			AvailableWorkspaces: available,
		}, nil
	}
	if debug {
		logging.Debug("refresh", fmt.Sprintf("task.List ok n=%d (+%s)", len(taskNames), time.Since(start).Round(time.Millisecond)))
	}

	targetCount := opts.TargetCount
	if targetCount <= 0 {
		targetCount = DefaultListTargetCount
	}

	idx, err := index.OpenDefault()
	if err != nil {
		logging.Error("refresh", "index.OpenDefault error: "+err.Error())
		return TaskListData{}, err
	}
	defer idx.Close()
	if debug {
		logging.Debug("refresh", fmt.Sprintf("index.OpenDefault ok (+%s)", time.Since(start).Round(time.Millisecond)))
	}

	if debug {
		logging.Debug("refresh", "index.Refresh() opts={GitOpenOnly}")
	}
	if err := idx.Refresh(ctx, index.RefreshPolicy{
		Git: index.GitPolicy{
			Mode: index.GitOpenOnly,
		},
	}); err != nil {
		logging.Error("refresh", "index.Refresh error: "+err.Error())
		return TaskListData{}, err
	}
	if debug {
		logging.Debug("refresh", fmt.Sprintf("index.Refresh ok (+%s)", time.Since(start).Round(time.Millisecond)))
	}

	var items []TaskListItem
	if opts.All {
		ls, err := idx.ListAll(ctx)
		if err != nil {
			logging.Error("refresh", "index.ListAll error: "+err.Error())
			return TaskListData{}, err
		}
		items = append(items, ls...)
	} else {
		open, err := idx.ListOpen(ctx)
		if err != nil {
			logging.Error("refresh", "index.ListOpen error: "+err.Error())
			return TaskListData{}, err
		}
		closed, err := idx.ListClosed(ctx)
		if err != nil {
			logging.Error("refresh", "index.ListClosed error: "+err.Error())
			return TaskListData{}, err
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
	if debug {
		logging.Debug("refresh", fmt.Sprintf("done items=%d (+%s)", len(items), time.Since(start).Round(time.Millisecond)))
	}
	return TaskListData{
		Items:               items,
		Workspaces:          workspaces,
		AvailableWorkspaces: available,
	}, nil
}

func countAvailableWorkspaces(items []TaskListItem, workspaces []workspace.Entry) int {
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
