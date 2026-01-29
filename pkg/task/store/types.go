package store

import (
	"context"
	"time"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/workflow"
	"github.com/zippoxer/subtask/pkg/workspace"
)

type Store interface {
	List(ctx context.Context, opts ListOptions) (ListResult, error)
	Get(ctx context.Context, name string, opts GetOptions) (TaskView, error)
}

type ListOptions struct {
	All bool
	// TargetCount only applies when All is false. If zero, the store uses a default.
	TargetCount int
}

type GetOptions struct{}

type ListResult struct {
	Tasks               []TaskListItem
	Errors              []TaskLoadError
	Workspaces          []workspace.Entry
	AvailableWorkspaces int
}

type TaskLoadError struct {
	Name string
	Err  error
}

type ChangesStatus string

const (
	ChangesStatusApplied ChangesStatus = "applied"
	ChangesStatusMissing ChangesStatus = "missing"
)

type Changes struct {
	Added   int
	Removed int
	Status  ChangesStatus
	Err     error
}

type Commits struct {
	Count int
	Err   error
}

type TaskListItem struct {
	Name         string
	Title        string
	FollowUp     string
	BaseBranch   string
	BaseCommit   string
	TaskStatus   task.TaskStatus
	WorkerStatus task.WorkerStatus
	Stage        string

	Workspace  string
	StartedAt  time.Time
	LastActive time.Time
	ToolCalls  int

	ProgressDone  int
	ProgressTotal int

	LastRunDurationMS int
	LastError         string

	Changes Changes
}

type TaskView struct {
	Task         *task.Task
	BaseCommit   string
	State        *task.State
	ProgressMeta *task.Progress
	Workflow     *workflow.Workflow

	TaskStatus   task.TaskStatus
	WorkerStatus task.WorkerStatus
	Stage        string

	LastHistoryNS int64
	LastRunMS     int

	Model     string
	Reasoning string

	ProgressSteps []task.ProgressStep
	TaskFiles     []string

	Changes Changes
	Commits Commits

	ConflictFiles []string
}
