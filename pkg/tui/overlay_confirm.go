package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderConfirmOverlay(m model, _ string) string {
	prompt := confirmPrompt(m.confirm)
	if prompt == "" {
		return ""
	}

	lines := []string{
		styleBold.Render("Confirm"),
		"",
		prompt,
		"",
		styleDim.Render("y confirm  n cancel"),
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

func confirmPrompt(c confirmState) string {
	if !c.active() {
		return ""
	}
	switch c.kind {
	case actionMerge:
		return "Merge " + styleBold.Render(c.taskName) + "? (y/n)"
	case actionClose:
		return "Close " + styleBold.Render(c.taskName) + "? (y/n)"
	case actionAbandon:
		return styleStatusError.Render("Abandon " + c.taskName + "? Discards changes. (y/n)")
	default:
		return ""
	}
}
