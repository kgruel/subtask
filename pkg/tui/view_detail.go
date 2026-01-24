package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
	"github.com/zippoxer/subtask/pkg/task"
)

// Box styles for detail view
var (
	detailBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(0, 1)

	detailBoxDimStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("238")).
				Padding(0, 1)
)

func renderDetailView(m model) string {
	leftPad := "  "
	contentWidth := m.width - len(leftPad) - 2
	if contentWidth < 40 {
		contentWidth = 40
	}

	var main strings.Builder
	var footer strings.Builder

	// Header: boxed with task name + status pill
	main.WriteString(renderDetailHeader(m, leftPad, contentWidth))
	main.WriteString("\n\n")

	// Tab bar
	main.WriteString(renderDetailTabBar(m, leftPad))
	main.WriteString("\n\n")

	// Content area
	if len(m.tasks) == 0 {
		main.WriteString(leftPad + styleDim.Render("No tasks."))
	} else if m.detailErr != nil {
		main.WriteString(leftPad + styleStatusError.Render(m.detailErr.Error()))
	} else if m.detailTaskName != m.selectedTaskName {
		main.WriteString(leftPad + styleDim.Render("Loading..."))
	} else {
		main.WriteString(renderDetailContent(m, leftPad, contentWidth))
	}

	// Status/toast line
	statusLine := renderStatusLine(m)
	if statusLine != "" {
		footer.WriteString(leftPad + statusLine)
		footer.WriteString("\n")
	}

	// Footer with keycaps
	help := styleKeycap.Render("Esc") + " back  " +
		styleKeycap.Render("←→") + " prev/next  " +
		styleKeycap.Render("1-4") + " tabs  " +
		styleKeycap.Render("↑↓") + " scroll/files  " +
		styleKeycap.Render("PgUp/Dn") + " page  " +
		styleKeycap.Render("^G") + " merge  " +
		styleKeycap.Render("q") + " quit"
	footer.WriteString(leftPad + help)

	return buildDetailFullHeight(m, main.String(), footer.String())
}

func renderDetailHeader(m model, leftPad string, contentWidth int) string {
	// Box adds 4 chars: 2 for borders + 2 for padding
	innerWidth := contentWidth - 4
	if innerWidth < 20 {
		innerWidth = 20
	}

	if len(m.tasks) == 0 || m.detail.Task == nil {
		box := detailBoxDimStyle.Width(innerWidth).Render("No task selected")
		return addPadding(box, leftPad)
	}

	tk := m.detail.Task

	// Task name + title on left, status pill on right
	name := styleBold.Render(tk.Name)
	var startedAt time.Time
	var lastError string
	if m.detail.State != nil {
		startedAt = m.detail.State.StartedAt
		lastError = m.detail.State.LastError
	}
	statusPill := statusPillStyled(m.detail.TaskStatus, m.detail.WorkerStatus, startedAt, m.detail.LastRunMS, lastError, m.spinnerFrame)

	// Build left side: name + title
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "240", Dark: "250"})
	left := name
	if strings.TrimSpace(tk.Title) != "" {
		left += titleStyle.Render("  " + tk.Title)
	}

	leftWidth := lipgloss.Width(left)
	statusWidth := lipgloss.Width(statusPill)

	// Truncate left if it doesn't fit
	maxLeftWidth := innerWidth - statusWidth - 2
	if leftWidth > maxLeftWidth && maxLeftWidth > 10 {
		// Truncate title, keep name
		nameWidth := lipgloss.Width(name)
		availForTitle := maxLeftWidth - nameWidth - 2 // 2 spaces
		if availForTitle > 3 && strings.TrimSpace(tk.Title) != "" {
			truncTitle := tk.Title
			if len(truncTitle) > availForTitle-3 {
				truncTitle = truncTitle[:availForTitle-3] + "..."
			}
			left = name + titleStyle.Render("  "+truncTitle)
		} else {
			left = name
		}
		leftWidth = lipgloss.Width(left)
	}

	gap := innerWidth - leftWidth - statusWidth
	if gap < 2 {
		gap = 2
	}
	headerLine := left + strings.Repeat(" ", gap) + statusPill

	box := detailBoxStyle.Render(headerLine)
	return addPadding(box, leftPad)
}

func renderDetailTabBar(m model, leftPad string) string {
	var parts []string
	for i := 0; i < int(tabCount); i++ {
		t := tab(i)

		// Skip Conflicts tab if no conflicts
		if t == tabConflicts && len(m.detail.ConflictFiles) == 0 {
			continue
		}

		label := t.Title()

		// Add file count for Changes tab
		if t == tabDiff && m.diffTaskName == m.selectedTaskName {
			label = fmt.Sprintf("%s (%d)", label, len(m.diffFiles))
		}

		if t == m.tab {
			parts = append(parts, zone.Mark(zoneTab(t), styleTabActive.Render(label)))
		} else {
			// Inactive: dim
			parts = append(parts, zone.Mark(zoneTab(t), styleTabInactive.Render(label)))
		}
	}

	return leftPad + strings.Join(parts, "   ")
}

func renderDetailContent(m model, leftPad string, contentWidth int) string {
	switch m.tab {
	case tabOverview:
		v := m.vpOverview.View()
		v = zone.Mark(zoneOverviewPane(), v)
		return addPadding(v, leftPad)
	case tabConversation:
		if m.conversationErr != nil && m.conversationTaskName == m.selectedTaskName {
			return leftPad + styleStatusError.Render(m.conversationErr.Error())
		}
		if m.conversationTaskName != m.selectedTaskName {
			return leftPad + styleDim.Render("Loading conversation...")
		}
		v := m.vpConversation.View()
		v = zone.Mark(zoneConversationPane(), v)
		return addPadding(v, leftPad)
	case tabDiff:
		return renderDiffView(m, leftPad)
	case tabConflicts:
		return addPadding(m.vpConflicts.View(), leftPad)
	default:
		return ""
	}
}

func buildDetailFullHeight(m model, mainContent, footerContent string) string {
	footerLines := strings.Split(strings.TrimSuffix(footerContent, "\n"), "\n")
	mainLines := strings.Split(strings.TrimSuffix(mainContent, "\n"), "\n")

	// Main area fills everything except footer
	mainHeight := m.height - len(footerLines)
	if mainHeight < 0 {
		mainHeight = 0
	}
	if len(mainLines) > mainHeight {
		mainLines = mainLines[:mainHeight]
	}
	for len(mainLines) < mainHeight {
		mainLines = append(mainLines, "")
	}

	if mainHeight == 0 {
		return strings.Join(footerLines, "\n")
	}
	return strings.Join(mainLines, "\n") + "\n" + strings.Join(footerLines, "\n")
}

func addPadding(content, leftPad string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = leftPad + line
	}
	return strings.Join(lines, "\n")
}

func statusPillStyled(taskStatus task.TaskStatus, workerStatus task.WorkerStatus, startedAt time.Time, lastRunMS int, lastError string, spinnerFrame int) string {
	return unifiedStatusTextStyled(taskStatus, workerStatus, startedAt, lastRunMS, lastError, spinnerFrame)
}

// formatDurationShort returns a short human-readable duration like "5m" or "2h".
func formatDurationShort(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m > 0 {
			return fmt.Sprintf("%dh%dm", h, m)
		}
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
