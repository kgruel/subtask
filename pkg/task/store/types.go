package store

import (
	"context"
	"time"

	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/workspace"
)

// Store is a read-oriented view over the task index. List and Get are
// query-shaped but are NOT pure reads: on the read path, for open tasks whose
// worker has started and is not currently running, they reconcile live git
// state into history.jsonl by appending durable task.merged events (when the
// branch tip is an ancestor of its base — an external merge) and task.commit
// events (as the branch advances). These appends are idempotent, lock-guarded,
// never lose work, and are gated so draft and running tasks are untouched.
// Treat List and Get as observe-and-reconcile, not side-effect-free.
type Store interface {
	List(ctx context.Context, opts ListOptions) (ListResult, error)
	Get(ctx context.Context, name string, opts GetOptions) (TaskView, error)
}

type ListOptions struct {
	All bool
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
	// Routine is non-nil for routine-driven tasks.
	Routine *routine.Routine

	TaskStatus   task.TaskStatus
	WorkerStatus task.WorkerStatus
	Stage        string

	LastHistoryNS int64
	LastRunMS     int

	Model     string
	Adapter   string
	Reasoning string

	ProgressSteps []task.ProgressStep
	TaskFiles     []string

	Changes Changes
	Commits Commits

	ConflictFiles []string
}
