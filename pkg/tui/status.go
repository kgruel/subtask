package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var floatingToastBg = lipgloss.NewStyle().
	Background(lipgloss.Color("236")).
	Foreground(lipgloss.Color("252")).
	Padding(1, 3).
	Bold(true)

var floatingToastBorder = lipgloss.NewStyle().
	Foreground(lipgloss.Color("75"))

func renderFloatingToast(text string) string {
	inner := floatingToastBg.Render(text)
	border := floatingToastBorder.Render("┃")
	lines := strings.Split(inner, "\n")
	for i, line := range lines {
		lines[i] = border + line + border
	}
	return strings.Join(lines, "\n")
}

func renderStatusLine(m model) string {
	if m.busy != actionNone && m.toast.text != "" {
		return styleDim.Render(m.toast.text)
	}
	if !m.toast.active() || m.toast.floating {
		return ""
	}
	switch m.toast.kind {
	case toastSuccess:
		return styleStatusWorking.Render(m.toast.text)
	case toastError:
		return styleStatusError.Render(m.toast.text)
	default:
		return styleDim.Render(m.toast.text)
	}
}

// overlayFloatingToast renders a floating toast in the top-right corner.
func overlayFloatingToast(view string, toast toastState, screenWidth, screenHeight int) string {
	if !toast.active() || !toast.floating || screenWidth <= 0 || screenHeight <= 0 {
		return view
	}

	// Render the toast box
	toastBox := renderFloatingToast(toast.text)
	toastW := lipgloss.Width(toastBox)

	// Position: top-right with margin
	margin := 2
	x := screenWidth - toastW - margin
	y := margin
	if x < 0 {
		x = 0
	}

	// Split both view and toast into lines
	viewLines := strings.Split(view, "\n")
	toastLines := strings.Split(toastBox, "\n")

	// Overlay toast lines onto view lines
	for i, toastLine := range toastLines {
		lineIdx := y + i
		if lineIdx < 0 || lineIdx >= len(viewLines) {
			continue
		}

		base := viewLines[lineIdx]

		// Use ansi.Truncate for left part, then append toast line padded to fill
		left := ansi.Truncate(base, x, "")
		leftW := ansi.StringWidth(left)

		// Pad left to reach x
		padding := ""
		if leftW < x {
			padding = strings.Repeat(" ", x-leftW)
		}

		// Compose: left + padding + toast (toast already has its own background)
		viewLines[lineIdx] = left + padding + toastLine
	}

	return strings.Join(viewLines, "\n")
}
