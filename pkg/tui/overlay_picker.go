package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type stageStepPickerState struct {
	taskName    string
	currentStep string
	options     []string
	selected    int
}

func (p stageStepPickerState) active() bool { return p.taskName != "" }

func (p stageStepPickerState) selection() (string, string) {
	if !p.active() || p.selected < 0 || p.selected >= len(p.options) {
		return "", ""
	}
	return p.taskName, p.options[p.selected]
}

func renderStagePickerOverlay(m model, _ string) string {
	if !m.stagePicker.active() {
		return ""
	}

	lines := []string{
		styleBold.Render("Stage advance for " + m.stagePicker.taskName),
		"",
		"Current: " + m.stagePicker.currentStep,
		"",
	}
	for i, option := range m.stagePicker.options {
		prefix := "  "
		if i == m.stagePicker.selected {
			prefix = styleSelectionIndicator.Render("> ")
			option = styleSelectedTaskName.Render(option)
		}
		lines = append(lines, prefix+option)
	}
	lines = append(lines,
		"",
		styleDim.Render("up/down pick   Enter confirm   Esc cancel"),
	)

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
