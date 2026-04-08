package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/ops"
)

type mergeDoneMsg struct {
	taskName   string
	baseBranch string
	res        ops.MergeResult
	err        error
}

type closeDoneMsg struct {
	taskName string
	abandon  bool
	res      ops.CloseResult
	err      error
}

func mergeTaskCmd(taskName string) tea.Cmd {
	return func() tea.Msg {
		baseBranch := ""
		msg := defaultMergeMessage(taskName)
		if tk, err := task.Load(taskName); err == nil && tk != nil {
			baseBranch = tk.BaseBranch
		}

		res, err := ops.MergeTask(taskName, msg, nil)
		return mergeDoneMsg{taskName: taskName, baseBranch: baseBranch, res: res, err: err}
	}
}

func closeTaskCmd(taskName string, abandon bool) tea.Cmd {
	return func() tea.Msg {
		res, err := ops.CloseTask(taskName, abandon, nil)
		return closeDoneMsg{taskName: taskName, abandon: abandon, res: res, err: err}
	}
}

func defaultMergeMessage(taskName string) string {
	tk, err := task.Load(taskName)
	if err != nil || tk == nil {
		return "Merge " + taskName
	}
	title := strings.TrimSpace(tk.Title)
	if title == "" {
		return "Merge " + taskName
	}
	if i := strings.IndexByte(title, '\n'); i >= 0 {
		title = strings.TrimSpace(title[:i])
	}
	if title == "" {
		return "Merge " + taskName
	}
	return title
}
