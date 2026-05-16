package render

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/x/ansi"
)

// TaskRow represents a task for display in the list.
type TaskRow struct {
	Name          string
	Status        string
	Stage         string
	Agent         string
	Progress      string // "X/Y" format
	LastActive    string
	Title         string
	LinesAdded    int // Git diff stats
	LinesRemoved  int
	ChangesStatus string // "", "applied", "missing"
	HasReview     bool   // True if task has at least one persisted review file
}

// TaskListTable renders a list of tasks.
type TaskListTable struct {
	Tasks         []TaskRow
	Footer        string
	TerminalWidth int
}

// RenderPlain renders the task list as plain text (for agents).
// It maintains a stable column count (including AGENT) regardless of width.
func (t *TaskListTable) RenderPlain() string {
	var buf strings.Builder
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "TASK\tSTATUS\tAGENT\tSTAGE\tPROGRESS\tCHANGES\tLAST ACTIVE\tTITLE")

	for _, task := range t.Tasks {
		progress := task.Progress
		if progress == "" {
			progress = "-"
		}
		changes := formatChangesForTask(task, false)
		title := task.Title
		if task.HasReview {
			title += " [reviewed]"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			task.Name, task.Status, task.Agent, task.Stage, progress, changes, task.LastActive, title)
	}

	if t.Footer != "" {
		fmt.Fprintf(w, "\t\t\t\t\t%s\n", t.Footer)
	}

	w.Flush()
	return buf.String()
}

// RenderPretty renders the task list with styling (for humans).
// Each task gets two lines: main info + title below.
func (t *TaskListTable) RenderPretty() string {
	if len(t.Tasks) == 0 {
		if t.Footer != "" {
			return styleDim.Render(t.Footer) + "\n"
		}
		return ""
	}

	showAgent := t.TerminalWidth == 0 || t.TerminalWidth >= 100

	// Headers (no TITLE - it goes on second line)
	headers := []string{"TASK", "STATUS"}
	if showAgent {
		headers = append(headers, "AGENT")
	}
	headers = append(headers, "STAGE", "PROGRESS", "CHANGES", "LAST ACTIVE")

	// Calculate column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, task := range t.Tasks {
		row := []string{
			task.Name,
			task.Status,
		}
		if showAgent {
			row = append(row, ansi.Truncate(task.Agent, 12, "…"))
		}
		row = append(row,
			task.Stage,
			formatProgressBar(task.Progress),
			formatChangesForTask(task, false), // Use plain for width calculation
			task.LastActive,
		)
		for i, cell := range row {
			// For progress bar and changes, use display width not byte length
			cellWidth := displayWidth(cell)
			if i < len(widths) && cellWidth > widths[i] {
				widths[i] = cellWidth
			}
		}
	}

	// Build content
	var lines []string

	// Header row
	headerCells := make([]string, len(headers))
	for i, h := range headers {
		headerCells[i] = styleTableHeader.Render(padRight(h, widths[i]))
	}
	lines = append(lines, strings.Join(headerCells, "  "))

	// Separator
	sepParts := make([]string, len(widths))
	for i, w := range widths {
		sepParts[i] = strings.Repeat("─", w)
	}
	lines = append(lines, styleDim.Render(strings.Join(sepParts, "──")))

	// Data rows (two lines per task + separator)
	for i, task := range t.Tasks {
		// Separator between tasks (not before first)
		if i > 0 {
			lines = append(lines, "")
		}

		// Main row
		cells := []string{
			padRight(task.Name, widths[0]),
			padRightDisplay(colorUnifiedStatus(task.Status), widths[1]),
		}
		idx := 2
		if showAgent {
			cells = append(cells, padRight(ansi.Truncate(task.Agent, 12, "…"), widths[idx]))
			idx++
		}
		cells = append(cells,
			padRight(task.Stage, widths[idx]),
			padRightDisplay(formatProgressBar(task.Progress), widths[idx+1]),
			padRightDisplay(formatChangesForTask(task, true), widths[idx+2]),
			padRight(task.LastActive, widths[idx+3]),
		)
		lines = append(lines, strings.Join(cells, "  "))

		// Title row (dimmed, aligned with task name)
		if task.Title != "" {
			title := task.Title
			if task.HasReview {
				title += "  [reviewed]"
			}
			titleLine := styleDim.Render("└ " + title)
			lines = append(lines, titleLine)
		}
	}

	content := strings.Join(lines, "\n")
	result := styleBox.Render(content)

	// Footer outside box
	if t.Footer != "" {
		result += "\n" + styleDim.Render(strings.Repeat(" ", 40)+t.Footer)
	}

	return result + "\n"
}

// formatChanges returns a plain "+X -Y" string for changes.
func formatChanges(added, removed int) string {
	if added == 0 && removed == 0 {
		return "-"
	}
	return fmt.Sprintf("+%d -%d", added, removed)
}

// formatChangesColored returns a colored "+X -Y" string for changes.
func formatChangesColored(added, removed int) string {
	if added == 0 && removed == 0 {
		return "-"
	}
	return styleSuccess.Render(fmt.Sprintf("+%d", added)) + " " + styleError.Render(fmt.Sprintf("-%d", removed))
}

// formatChangesForTask returns the changes column content based on task status.
// For replied tasks: shows changes + merge hint
// For closed+merged tasks: shows "✓ merged" in purple
// For other tasks: shows normal changes
func formatChangesForTask(task TaskRow, colored bool) string {
	switch strings.TrimSpace(task.ChangesStatus) {
	case "missing":
		if colored {
			return styleDim.Render("missing")
		}
		return "missing"
	case "applied":
		if colored {
			return styleDim.Render("applied")
		}
		return fmt.Sprintf("applied (+%d -%d)", task.LinesAdded, task.LinesRemoved)
	}

	// Normal changes
	var changes string
	if colored {
		changes = formatChangesColored(task.LinesAdded, task.LinesRemoved)
	} else {
		changes = formatChanges(task.LinesAdded, task.LinesRemoved)
	}

	return changes
}

func colorTaskStatus(status string) string {
	return colorUnifiedStatus(status)
}

func colorUnifiedStatus(status string) string {
	s := strings.TrimSpace(status)
	switch {
	case strings.HasPrefix(s, "working"):
		return styleStatusWorking.Render(s)
	case strings.HasPrefix(s, "running"):
		return styleStatusWorking.Render(s)
	case strings.HasPrefix(s, "replied"):
		return styleStatusReplied.Render(s)
	case strings.HasPrefix(s, "error"):
		return styleStatusError.Render(s)
	case strings.Contains(s, "merged"):
		return styleStatusMerged.Render(s)
	case s == "closed":
		return styleStatusClosed.Render(s)
	case s == "—" || s == "-" || s == "":
		return styleDim.Render("—")
	default:
		return styleDim.Render(s)
	}
}

// Print renders and prints the task list.
func (t *TaskListTable) Print() {
	if Pretty {
		fmt.Print(t.RenderPretty())
	} else {
		fmt.Print(t.RenderPlain())
	}
}

// formatProgressBar converts "X/Y" to a progress bar in pretty mode.
func formatProgressBar(progress string) string {
	if progress == "" || progress == "-" {
		return "-"
	}

	parts := strings.Split(progress, "/")
	if len(parts) != 2 {
		return progress
	}

	done, err1 := strconv.Atoi(parts[0])
	total, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || total == 0 {
		return progress
	}

	// Build bar (5 chars wide)
	filled := (done * 5) / total
	empty := 5 - filled
	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

	return styleSuccess.Render(bar) + " " + progress
}

// displayWidth returns the visible width of a string (ignoring ANSI codes).
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

// padRightDisplay pads a string to the given display width (accounting for ANSI).
func padRightDisplay(s string, width int) string {
	dw := displayWidth(s)
	if dw >= width {
		return s
	}
	return s + strings.Repeat(" ", width-dw)
}
