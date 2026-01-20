package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/gather"
)

// Selection indicator character
const selectionIndicator = "▶"

// Column widths (will be calculated dynamically)
var listHeaders = []string{"Task", "Status", "Stage", "Changes", "Progress", "Activity"}

func renderListView(m model) string {
	// Left padding for content (backgrounds extend full width)
	leftPad := "  "
	contentWidth := m.width - len(leftPad)
	if contentWidth < 40 {
		contentWidth = 40
	}

	var main strings.Builder
	var footer strings.Builder

	// Search bar: only shown when search is active (3 lines with bg)
	searchBarLines := 0
	if m.searchActive {
		searchBg := lipgloss.AdaptiveColor{Light: "254", Dark: "238"}
		searchBgStyle := lipgloss.NewStyle().Background(searchBg)

		sLine1 := searchBgStyle.Width(m.width).Render("") // empty
		searchBox := renderSearchBoxWithBg(m, contentWidth, searchBg)
		sLine2 := searchBgStyle.Width(m.width).Render(leftPad + searchBox)
		sLine3 := searchBgStyle.Width(m.width).Render("") // empty

		main.WriteString(sLine1 + "\n" + sLine2 + "\n" + sLine3 + "\n")
		searchBarLines = 3
	}

	if m.listErr != nil {
		main.WriteString("\n")
		main.WriteString(leftPad + styleStatusError.Render(m.listErr.Error()))
		footer.WriteString(leftPad + styleDim.Render("q quit"))
		return buildFullHeightView(m, main.String(), footer.String())
	}

	tasks := m.visibleTasks()

	if len(tasks) == 0 {
		if m.searchActive && m.searchInput.Value() != "" {
			main.WriteString("\n")
			main.WriteString(leftPad + center(contentWidth, "No matching tasks."))
			main.WriteString("\n\n")
			main.WriteString(leftPad + center(contentWidth, "Press Esc to clear search"))
		} else {
			main.WriteString("\n")
			main.WriteString(leftPad + center(contentWidth, "No tasks yet."))
			main.WriteString("\n\n")
			main.WriteString(leftPad + center(contentWidth, "Create one with: subtask draft <name>"))
		}
		footer.WriteString(leftPad + styleDim.Render("q quit  ? help"))
		return buildFullHeightView(m, main.String(), footer.String())
	}

	// Calculate column widths (TASK through CHANGES only; PROGRESS stretches, LAST ACTIVE is right-aligned).
	widths := make([]int, 4)
	for i := range widths {
		widths[i] = len(listHeaders[i])
	}
	for _, t := range tasks {
		row := listRowDataLeft(t)
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Table header
	headerContent := buildHeaderRow(widths, contentWidth)
	main.WriteString("\n") // empty line before header
	main.WriteString(leftPad + headerContent + "\n")
	main.WriteString("\n") // empty line after header

	// Calculate visible rows
	// Reserve: search bar (0 or 3) + blank (1) + table header (1) + blank (1) + footer (2)
	reservedLines := searchBarLines + 1 + 1 + 1 + 2
	// Each task: 2 lines (row + title) + 1 blank between = 3 lines, except last has no trailing blank
	maxVisibleTasks := (m.height - reservedLines + 1) / 3 // +1 because last task has no trailing blank
	if maxVisibleTasks < 1 {
		maxVisibleTasks = 1
	}

	// Adjust offset to keep selection visible
	if m.selected < m.offset {
		m.offset = m.selected
	} else if m.selected >= m.offset+maxVisibleTasks {
		m.offset = m.selected - maxVisibleTasks + 1
	}

	start := m.offset
	end := min(len(tasks), start+maxVisibleTasks)

	// Data rows
	for i := start; i < end; i++ {
		t := tasks[i]

		// Blank line between tasks (not before first)
		if i > start {
			main.WriteString("\n")
		}

		// Title line
		titleLine := ""
		if t.Title != "" {
			titleLine = "└ " + t.Title
		}

		// Selection uses indicator + blue task name (no background)
		if i == m.selected {
			// Indicator prefix replaces first char of leftPad
			indicator := styleSelectionIndicator.Render(selectionIndicator) + " "
			row := buildTaskRowSelected(t, widths, contentWidth, m.spinnerFrame)
			main.WriteString(zone.Mark(zoneTaskRow(t.Name), indicator+row))
			main.WriteString("\n")
			if titleLine != "" {
				main.WriteString(leftPad + styleDim.Render(titleLine))
				main.WriteString("\n")
			}
		} else {
			// Normal row (no indicator)
			row := buildTaskRow(t, widths, contentWidth, m.spinnerFrame)
			main.WriteString(zone.Mark(zoneTaskRow(t.Name), leftPad+row))
			main.WriteString("\n")
			if titleLine != "" {
				main.WriteString(leftPad + styleDim.Render(titleLine))
				main.WriteString("\n")
			}
		}
	}

	// Status/toast line (part of footer, stuck to bottom)
	statusLine := renderStatusLine(m)
	if statusLine != "" {
		footer.WriteString(leftPad + statusLine)
		footer.WriteString("\n")
	}

	// Help line with keycap styling
	help := styleKeycap.Render("/") + " search  " +
		styleKeycap.Render("↑↓") + " navigate  " +
		styleKeycap.Render("Enter") + " view  " +
		styleKeycap.Render("^G") + " merge  " +
		styleKeycap.Render("^D") + " close  " +
		styleKeycap.Render("^X") + " abandon  " +
		styleKeycap.Render("?") + " help  " +
		styleKeycap.Render("q") + " quit"
	footer.WriteString(leftPad + help)

	return buildFullHeightView(m, main.String(), footer.String())
}

// buildFullHeightView creates a full-height layout with content at top, footer at bottom.
func buildFullHeightView(m model, mainContent, footerContent string) string {
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

// buildHeaderRow builds table header row.
func buildHeaderRow(widths []int, totalWidth int) string {
	// Helper: pad header text
	pad := func(text string, width int) string {
		styled := styleTableHeader.Render(text)
		dw := displayWidth(styled)
		if dw >= width {
			return styled
		}
		return styled + strings.Repeat(" ", width-dw)
	}

	cells := []string{
		pad(listHeaders[0], widths[0]), // Task
		pad(listHeaders[1], widths[1]), // Status
		pad(listHeaders[2], widths[2]), // Stage
		pad(listHeaders[3], widths[3]), // Changes
	}
	leftPart := strings.Join(cells, "  ")
	leftWidth := displayWidth(leftPart)

	progressHeader := styleTableHeader.Render(listHeaders[4])   // Progress
	lastActiveHeader := styleTableHeader.Render(listHeaders[5]) // Activity

	progressWidth := displayWidth(progressHeader)
	lastActiveWidth := displayWidth(lastActiveHeader)
	gap := totalWidth - leftWidth - 2 - progressWidth - 2 - lastActiveWidth
	if gap < 2 {
		gap = 2
	}

	return leftPart + "  " + progressHeader + strings.Repeat(" ", gap) + lastActiveHeader
}

// renderSearchBoxWithBg renders search box with given background color.
func renderSearchBoxWithBg(m model, maxWidth int, bg lipgloss.TerminalColor) string {
	if maxWidth < 10 {
		maxWidth = 10
	}

	bgStyle := lipgloss.NewStyle().Background(bg)

	// Dim "/" prefix
	prefix := styleDim.Background(bg).Render("/")

	if m.searchActive {
		value := m.searchInput.Value()
		if len(value) > maxWidth-5 {
			value = value[:maxWidth-5]
		}
		cursor := "█"
		return prefix + bgStyle.Render(" "+value+cursor)
	}

	// Inactive: show placeholder
	placeholder := styleDim.Background(bg).Render("filter...")
	return prefix + bgStyle.Render(" ") + placeholder
}

// stageText returns stage or empty for closed tasks.
func stageText(t gather.TaskListItem) string {
	return t.Stage
}

// listRowDataLeft returns plain text data for left-column width calculation.
func listRowDataLeft(t gather.TaskListItem) []string {
	return []string{
		t.Name,
		unifiedStatusTextPlain(t.TaskStatus, t.WorkerStatus, t.IntegratedReason, t.StartedAt, t.LastRunDurationMS, t.LastError),
		stageText(t),
		changesTextPlain(t),
	}
}

// buildTaskRow builds a complete row with stretched layout.
// PROGRESS column stretches to fill space, LAST ACTIVE is right-aligned.
func buildTaskRow(t gather.TaskListItem, widths []int, totalWidth int, spinnerFrame int) string {
	// Build left columns (TASK through CHANGES)
	leftCells := []string{
		padRight(t.Name, widths[0]),
		padRightDisplay(unifiedStatusTextStyled(t.TaskStatus, t.WorkerStatus, t.IntegratedReason, t.StartedAt, t.LastRunDurationMS, t.LastError, spinnerFrame), widths[1]),
		padRight(stageText(t), widths[2]),
		padRightDisplay(changesTextStyled(t), widths[3]),
	}
	leftPart := strings.Join(leftCells, "  ")

	// PROGRESS and LAST ACTIVE
	progressPart := progressBar(t.ProgressDone, t.ProgressTotal)
	lastActivePart := lastActiveText(t)

	// Calculate gap between PROGRESS and LAST ACTIVE
	leftWidth := displayWidth(leftPart)
	progressWidth := displayWidth(progressPart)
	lastActiveWidth := len(lastActivePart)

	// Gap fills remaining space
	gap := totalWidth - leftWidth - 2 - progressWidth - 2 - lastActiveWidth
	if gap < 2 {
		gap = 2
	}

	return leftPart + "  " + progressPart + strings.Repeat(" ", gap) + styleDim.Render(lastActivePart)
}

// buildTaskRowSelected builds a row for selected task with blue+bold task name.
func buildTaskRowSelected(t gather.TaskListItem, widths []int, totalWidth int, spinnerFrame int) string {
	// Build left columns - task name is blue+bold, rest normal
	leftCells := []string{
		styleSelectedTaskName.Render(padRight(t.Name, widths[0])),
		padRightDisplay(unifiedStatusTextStyled(t.TaskStatus, t.WorkerStatus, t.IntegratedReason, t.StartedAt, t.LastRunDurationMS, t.LastError, spinnerFrame), widths[1]),
		padRight(stageText(t), widths[2]),
		padRightDisplay(changesTextStyled(t), widths[3]),
	}
	leftPart := strings.Join(leftCells, "  ")

	// PROGRESS and LAST ACTIVE
	progressPart := progressBar(t.ProgressDone, t.ProgressTotal)
	lastActivePart := lastActiveText(t)

	// Calculate gap between PROGRESS and LAST ACTIVE
	leftWidth := displayWidth(leftPart)
	progressWidth := displayWidth(progressPart)
	lastActiveWidth := len(lastActivePart)

	// Gap fills remaining space
	gap := totalWidth - leftWidth - 2 - progressWidth - 2 - lastActiveWidth
	if gap < 2 {
		gap = 2
	}

	return leftPart + "  " + progressPart + strings.Repeat(" ", gap) + styleDim.Render(lastActivePart)
}

func unifiedStatusTextPlain(ts task.TaskStatus, ws task.WorkerStatus, integratedReason string, startedAt time.Time, lastRunMS int, lastError string) string {
	// Don't show "merged" if worker is actively running
	if ws != task.WorkerStatusRunning && strings.TrimSpace(integratedReason) != "" {
		return "✓ merged"
	}
	switch task.UserStatusFor(ts, ws) {
	case task.UserStatusMerged:
		return "✓ merged"
	case task.UserStatusClosed:
		return "closed"
	case task.UserStatusRunning:
		if !startedAt.IsZero() {
			return "◐ working (" + formatDurationShort(time.Since(startedAt)) + ")"
		}
		return "◐ working"
	case task.UserStatusReplied:
		if lastRunMS > 0 {
			return "○ replied (" + formatDurationShort(time.Duration(lastRunMS)*time.Millisecond) + ")"
		}
		return "○ replied"
	case task.UserStatusError:
		if lastError == "interrupted" {
			if lastRunMS > 0 {
				return "⊘ interrupted (" + formatDurationShort(time.Duration(lastRunMS)*time.Millisecond) + ")"
			}
			return "⊘ interrupted"
		}
		if lastRunMS > 0 {
			return "✗ error (" + formatDurationShort(time.Duration(lastRunMS)*time.Millisecond) + ")"
		}
		return "✗ error"
	case task.UserStatusDraft:
		return "draft"
	default:
		return "—"
	}
}

func unifiedStatusTextStyled(ts task.TaskStatus, ws task.WorkerStatus, integratedReason string, startedAt time.Time, lastRunMS int, lastError string, spinnerFrame int) string {
	// Don't show "merged" if worker is actively running
	if ws != task.WorkerStatusRunning && strings.TrimSpace(integratedReason) != "" {
		return styleStatusMerged.Render("✓ merged")
	}
	switch task.UserStatusFor(ts, ws) {
	case task.UserStatusMerged:
		return styleStatusMerged.Render("✓ merged")
	case task.UserStatusClosed:
		return styleStatusClosed.Render("closed")
	case task.UserStatusRunning:
		spinner := spinnerFrames[spinnerFrame%len(spinnerFrames)]
		s := spinner + " working"
		if !startedAt.IsZero() {
			s += " (" + formatDurationShort(time.Since(startedAt)) + ")"
		}
		return styleStatusWorking.Render(s)
	case task.UserStatusReplied:
		s := "○ replied"
		if lastRunMS > 0 {
			s += " (" + formatDurationShort(time.Duration(lastRunMS)*time.Millisecond) + ")"
		}
		return styleStatusReplied.Render(s)
	case task.UserStatusError:
		if lastError == "interrupted" {
			s := "⊘ interrupted"
			if lastRunMS > 0 {
				s += " (" + formatDurationShort(time.Duration(lastRunMS)*time.Millisecond) + ")"
			}
			return styleStatusInterrupted.Render(s)
		}
		s := "✗ error"
		if lastRunMS > 0 {
			s += " (" + formatDurationShort(time.Duration(lastRunMS)*time.Millisecond) + ")"
		}
		return styleStatusError.Render(s)
	case task.UserStatusDraft:
		return styleDim.Render("draft")
	default:
		return styleDim.Render("—")
	}
}

func progressBar(done, total int) string {
	if total <= 0 {
		return ""
	}
	if done < 0 {
		done = 0
	}
	if done > total {
		done = total
	}

	const barWidth = 6

	// Scale proportionally with rounding
	filledCount := (done*barWidth + total/2) / total
	if filledCount > barWidth {
		filledCount = barWidth
	}

	filled := strings.Repeat("━", filledCount)
	empty := strings.Repeat("─", barWidth-filledCount)

	filledStyle := lipgloss.NewStyle().Foreground(colorGreen)
	emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "250", Dark: "243"})

	return filledStyle.Render(filled) + emptyStyle.Render(empty)
}

func changesTextPlain(t gather.TaskListItem) string {
	if t.LinesAdded == 0 && t.LinesRemoved == 0 {
		return ""
	}
	return fmt.Sprintf("+%d -%d", t.LinesAdded, t.LinesRemoved)
}

func changesTextStyled(t gather.TaskListItem) string {
	if t.LinesAdded == 0 && t.LinesRemoved == 0 {
		return ""
	}
	return styleSuccess.Render(fmt.Sprintf("+%d", t.LinesAdded)) + " " +
		styleError.Render(fmt.Sprintf("-%d", t.LinesRemoved))
}

func lastActiveText(t gather.TaskListItem) string {
	if t.LastActive.IsZero() {
		return ""
	}
	return formatTimeAgo(t.LastActive)
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func padRightDisplay(s string, width int) string {
	dw := displayWidth(s)
	if dw >= width {
		return s
	}
	return s + strings.Repeat(" ", width-dw)
}

func displayWidth(s string) int {
	// Strip ANSI escape codes for width calculation
	inEscape := false
	width := 0
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		width++
	}
	return width
}

func center(width int, s string) string {
	if width <= 0 {
		return s
	}
	if len(s) >= width {
		return s
	}
	left := (width - len(s)) / 2
	return strings.Repeat(" ", left) + s
}
