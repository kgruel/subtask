package gather

import (
	"context"
	"encoding/json"
	"os"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/index"
	"github.com/zippoxer/subtask/pkg/workflow"
	"github.com/zippoxer/subtask/pkg/workspace"
)

type TaskDetail struct {
	Task         *task.Task
	State        *task.State
	ProgressMeta *task.Progress
	Workflow     *workflow.Workflow

	TaskStatus   task.TaskStatus
	WorkerStatus task.WorkerStatus
	Stage        string
	LastHistory  int64 // unix nanos (for consumers that want stable sorts)
	LastRunMS    int

	Model     string
	Reasoning string

	ProgressSteps []task.ProgressStep
	TaskFiles     []string

	LinesAdded    int
	LinesRemoved  int
	CommitsBehind int
	ConflictFiles []string

	IntegratedReason string
}

func Detail(ctx context.Context, taskName string) (TaskDetail, error) {
	idx, err := index.OpenDefault()
	if err != nil {
		return TaskDetail{}, err
	}
	defer idx.Close()

	if err := idx.Refresh(ctx, index.RefreshPolicy{
		Git: index.GitPolicy{
			Mode:               index.GitTasks,
			Tasks:              []string{taskName},
			IncludeConflicts:   true,
			IncludeIntegration: true,
		},
	}); err != nil {
		return TaskDetail{}, err
	}

	rec, ok, err := idx.Get(ctx, taskName)
	if err != nil {
		return TaskDetail{}, err
	}
	if !ok || rec.Task == nil {
		// Preserve historical errors for missing/invalid tasks.
		_, err := task.Load(taskName)
		return TaskDetail{}, err
	}

	t := rec.Task
	state := rec.State
	meta := rec.ProgressMeta
	cfg, _ := workspace.LoadConfig() // best-effort (allows working in partial setups)

	d := TaskDetail{
		Task:          t,
		State:         state,
		ProgressMeta:  meta,
		ProgressSteps: task.LoadProgressSteps(taskName),
		Model:         workspace.ResolveModel(cfg, t, ""),
		TaskStatus:    rec.TaskStatus,
		WorkerStatus:  rec.WorkerStatus,
		Stage:         rec.Stage,
		LastHistory:   rec.LastHistory.UnixNano(),
		LastRunMS:     rec.LastRunDurationMS,
	}
	d.IntegratedReason = rec.IntegratedReason
	if cfg != nil && cfg.Harness == "codex" {
		d.Reasoning = workspace.ResolveReasoning(cfg, t, "")
	}

	d.LinesAdded = rec.LinesAdded
	d.LinesRemoved = rec.LinesRemoved
	d.CommitsBehind = rec.CommitsBehind

	if rec.ConflictFilesJSON != "" {
		var conflicts []string
		if err := json.Unmarshal([]byte(rec.ConflictFilesJSON), &conflicts); err == nil && len(conflicts) > 0 {
			d.ConflictFiles = conflicts
		}
	}

	// Workflow for this task, if any.
	if wf, err := workflow.LoadFromTask(taskName); err == nil {
		d.Workflow = wf
	}

	// Task folder files.
	taskDir := task.Dir(taskName)
	entries, err := os.ReadDir(taskDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				d.TaskFiles = append(d.TaskFiles, e.Name())
			}
		}
	}

	return d, nil
}
