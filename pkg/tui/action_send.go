package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kgruel/subtask/pkg/task"
)

func (m *model) openSendInput() tea.Cmd {
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
	m.sendInput = newSendInputState(m.selectedTaskName, m.width)
	return nil
}

func (m model) selectedTaskStatus() task.TaskStatus {
	if m.detailTaskName == m.selectedTaskName && m.detail.TaskStatus != "" {
		return m.detail.TaskStatus
	}
	for _, item := range m.tasks {
		if item.Name == m.selectedTaskName {
			return item.TaskStatus
		}
	}
	return task.TaskStatusOpen
}

func (m model) selectedWorkerStatus() task.WorkerStatus {
	if m.detailTaskName == m.selectedTaskName && m.detail.WorkerStatus != "" {
		return m.detail.WorkerStatus
	}
	for _, item := range m.tasks {
		if item.Name == m.selectedTaskName {
			return item.WorkerStatus
		}
	}
	return task.WorkerStatusNotStarted
}

func wouldSendText(taskName string) string {
	return "would send " + taskName
}
