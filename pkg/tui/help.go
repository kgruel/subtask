package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func renderHelpOverlay(m model, _ string) string {
	lines := []string{
		styleBold.Render("Subtask"),
		"",
		styleBold.Render("Navigation"),
		"  ↑/↓, j/k    Navigate tasks (list)",
		"  Enter       View task details",
		"  Esc         Back to list",
		"  ←/→         Switch task (detail)",
		"  1-5         Switch tabs (detail)",
		"  Tab         Next tab",
		"  Shift+Tab   Previous tab",
		"  PgUp/Dn     Page up/down",
		"  g           Go to top (list)",
		"  /           Search",
		"  s           Side-by-side diff",
		"  Ctrl+G      Merge task",
		"  Ctrl+D      Close task",
		"  Ctrl+X      Abandon task",
		"  q           Quit",
		"",
		styleBold.Render("Mouse"),
		"  Click       Select task / tab / file",
		"  Double-click  Open task (list)",
		"  Scroll      Scroll content",
		"",
		styleDim.Render("Tip: Hold Shift while dragging for native text selection."),
		"",
		styleDim.Render("Press Esc or ? to close help."),
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
