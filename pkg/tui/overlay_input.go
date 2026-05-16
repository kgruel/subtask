package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
)

type sendInputState struct {
	taskName string
	input    textarea.Model
}

func (s sendInputState) active() bool { return s.taskName != "" }

func newSendInputState(taskName string, width int) sendInputState {
	input := textarea.New()
	input.Focus()
	input.Placeholder = "Type a prompt for the worker..."
	input.ShowLineNumbers = false
	input.SetWidth(max(30, min(width-10, 72)))
	input.SetHeight(6)
	return sendInputState{taskName: taskName, input: input}
}

func renderSendInputOverlay(m model, _ string) string {
	if !m.sendInput.active() {
		return ""
	}

	title := "Send to " + m.sendInput.taskName
	lines := []string{
		styleBold.Render(title),
		"",
		m.sendInput.input.View(),
		"",
		styleDim.Render("Ctrl+S send   Esc cancel"),
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		BorderForeground(lipgloss.AdaptiveColor{Light: "245", Dark: "245"}).
		Render(strings.Join(lines, "\n"))

	return lipgloss.Place(
		max(0, m.width),
		max(0, m.height),
		lipgloss.Center,
		lipgloss.Center,
		box,
	)
}
