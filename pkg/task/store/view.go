package store

import (
	"context"
	"strings"
	"time"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/workspace"
)

// BuildViewOptions allows overriding parts of the task view during construction.
type BuildViewOptions struct {
	Stage string // override current routine stage
}

// BuildView gathers all data needed for a unified task view.
// If cfg is non-nil, it is used for adapter/model resolution; otherwise
// the default project config is loaded from disk.
func BuildView(ctx context.Context, name string, cfg *workspace.Config, opts BuildViewOptions) (*task.View, error) {
	st := &store{}
	tv, err := st.getWithConfig(ctx, name, cfg)
	if err != nil {
		return nil, err
	}
	if opts.Stage != "" {
		tv.Stage = opts.Stage
	}
	return BuildViewFromTaskView(tv)
}

// BuildViewFromTaskView converts a TaskView (internal store model) to a task.View (UI model).
func BuildViewFromTaskView(tv TaskView) (*task.View, error) {
	t := tv.Task
	state := tv.State

	lastError := ""
	startedAt := time.Time{}
	workspacePath := ""
	if state != nil {
		lastError = state.LastError
		startedAt = state.StartedAt
		workspacePath = state.Workspace
	}

	v := &task.View{
		Name:         t.Name,
		Title:        t.Title,
		Branch:       t.Name,
		BaseBranch:   t.BaseBranch,
		Status:       tv.TaskStatus,
		WorkerStatus: tv.WorkerStatus,
		IsTerminal:   tv.TaskStatus == task.TaskStatusMerged || tv.TaskStatus == task.TaskStatusClosed,
		StatusText:   task.UserStatusText(tv.TaskStatus, tv.WorkerStatus, startedAt, tv.LastRunMS, lastError, time.Now()),
		BaseCommit:   tv.BaseCommit,
		Workspace:    workspacePath,
		TaskDir:      task.Dir(t.Name),
		TaskFiles:    tv.TaskFiles,
		Conflicts:    tv.ConflictFiles,
	}

	if state != nil && tv.WorkerStatus == task.WorkerStatusError && strings.TrimSpace(state.LastError) != "" {
		v.Error = state.LastError
	}

	// Agent resolution
	var stepAgent string
	var stepInstructions string
	if tv.Routine != nil {
		if strings.TrimSpace(tv.Stage) != "" {
			if step := tv.Routine.GetStep(tv.Stage); step != nil {
				stepAgent = step.Agent
				stepInstructions = step.Instructions
			}
		}
	}

	v.Agent = task.AgentView{
		Name:      stepAgent,
		Adapter:   tv.Adapter,
		Model:     tv.Model,
		Reasoning: tv.Reasoning,
	}
	if v.Agent.Name == "" {
		v.Agent.Name = t.Agent
	}

	// Changes
	v.Changes = task.ChangesView{
		Added:   tv.Changes.Added,
		Removed: tv.Changes.Removed,
		Status:  string(tv.Changes.Status),
		Err:     tv.Changes.Err,
	}

	// Commits
	v.Commits = task.CommitsView{
		Count: tv.Commits.Count,
		Err:   tv.Commits.Err,
		Show:  tv.TaskStatus == task.TaskStatusOpen,
	}

	// Reviews
	v.Reviews = task.LoadReviewSummary(t.Name)

	// Routine
	if tv.Routine != nil {
		v.Routine = &task.RoutineView{
			Name:         tv.Routine.Name,
			Source:       tv.Routine.Source,
			CurrentStep:  tv.Stage,
			StepAgent:    stepAgent,
			Instructions: stepInstructions,
		}
		// Convert steps for diagram rendering
		v.Routine.Steps = make([]task.StepView, len(tv.Routine.Steps))
		for i, s := range tv.Routine.Steps {
			sv := task.StepView{
				ID:    s.ID,
				Kind:  string(s.Kind),
				Agent: s.Agent,
			}
			if len(s.Options) > 0 {
				sv.Options = make([]task.OptionView, len(s.Options))
				for j, o := range s.Options {
					sv.Options[j] = task.OptionView{Name: o.Name, Next: o.Next}
				}
			}
			if len(s.Branches) > 0 {
				sv.Branches = make([]task.BranchView, len(s.Branches))
				for j, b := range s.Branches {
					sv.Branches[j] = task.BranchView{Field: b.Field, To: b.To}
				}
			}
			v.Routine.Steps[i] = sv
		}
	}

	// Artifacts
	if arts, err := task.Artifacts(t.Name); err == nil {
		v.Artifacts = arts
	}

	// Progress Steps
	if len(tv.ProgressSteps) > 0 {
		v.ProgressSteps = make([]task.ProgressStep, len(tv.ProgressSteps))
		copy(v.ProgressSteps, tv.ProgressSteps)
	}

	return v, nil
}
