package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/store"
)

func (m *model) advanceStage() tea.Cmd {
	if m.selectedTaskName == "" {
		return nil
	}
	if status := m.selectedTaskStatus(); status != task.TaskStatusOpen {
		m.toast = toastState{kind: toastInfo, text: "task is " + string(status), until: nowFunc().Add(3 * time.Second)}
		return nil
	}
	if m.selectedWorkerStatus() == task.WorkerStatusRunning {
		m.toast = toastState{kind: toastInfo, text: "worker already running", until: nowFunc().Add(3 * time.Second)}
		return nil
	}
	if m.detailTaskName != m.selectedTaskName || m.detail.Task == nil {
		m.toast = toastState{kind: toastInfo, text: "task details still loading", until: nowFunc().Add(3 * time.Second)}
		return nil
	}

	res, err := resolveStageAdvance(m.detail)
	if err != nil {
		m.toast = toastState{kind: toastError, text: err.Error(), until: nowFunc().Add(4 * time.Second)}
		return nil
	}
	switch {
	case len(res.gateOptions) > 0:
		m.stagePicker = stageStepPickerState{
			taskName:    m.selectedTaskName,
			currentStep: res.currentStep,
			options:     res.gateOptions,
		}
	case res.targetStep != "":
		return m.spawnSubtask(m.selectedTaskName, []string{"stage", m.selectedTaskName, res.targetStep})
	}
	return nil
}

func (m model) spawnSubtask(taskName string, args []string) tea.Cmd {
	if m.subtaskSpawner == nil {
		return nil
	}
	return m.subtaskSpawner(taskName, args)
}

type stageAdvanceResolution struct {
	currentStep string
	targetStep  string
	gateOptions []string
}

func resolveStageAdvance(detail store.TaskView) (stageAdvanceResolution, error) {
	if detail.Task == nil || detail.Task.Routine == "" || detail.Routine == nil {
		return stageAdvanceResolution{}, stageAdvanceError("task has no routine")
	}
	r := detail.Routine
	currentID := detail.Stage
	if currentID == "" {
		currentID = r.EntryStep()
	}
	current := r.GetStep(currentID)
	if current == nil {
		return stageAdvanceResolution{}, stageAdvanceError("no current step in routine")
	}
	if current.Kind == routine.KindTerminal {
		return stageAdvanceResolution{}, stageAdvanceError("task is at terminal step")
	}
	if current.Kind == routine.KindGate {
		options := make([]string, 0, len(current.Options))
		for _, option := range current.Options {
			options = append(options, option.Next)
		}
		if len(options) == 0 {
			return stageAdvanceResolution{}, stageAdvanceError("no gate options in routine")
		}
		return stageAdvanceResolution{currentStep: current.ID, gateOptions: options}, nil
	}
	next := nextStepInOrder(r, current.ID)
	if next == "" {
		return stageAdvanceResolution{}, stageAdvanceError("no next step in routine")
	}
	return stageAdvanceResolution{currentStep: current.ID, targetStep: next}, nil
}

type stageAdvanceError string

func (e stageAdvanceError) Error() string { return string(e) }

func nextStepInOrder(r *routine.Routine, currentID string) string {
	for i := range r.Steps {
		if r.Steps[i].ID == currentID && i < len(r.Steps)-1 {
			return r.Steps[i+1].ID
		}
	}
	return ""
}
