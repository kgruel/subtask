package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderAlertOverlay(m model, _ string) string {
	if !m.alert.active() {
		return ""
	}

	title := m.alert.title
	if strings.TrimSpace(title) == "" {
		title = "Message"
	}

	lines := []string{
		styleStatusError.Render(title),
		"",
		strings.TrimSpace(m.alert.body),
		"",
		styleDim.Render("Press Esc to close."),
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
