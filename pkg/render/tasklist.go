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
	Name string
	// Status is the display text (e.g. "working (3m)", "✓ merged").
	Status string
	// UserStatus is the task.UserStatus value ("draft"|"working"|"replied"|
	// "error"|"merged"|"closed") that produced Status. Color is selected from
	// this enum, not by re-parsing Status, so text and color cannot drift.
	UserStatus    string
	Stage         string
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
func (t *TaskListTable) RenderPlain() string {
	var buf strings.Builder
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "TASK\tSTATUS\tSTAGE\tPROGRESS\tCHANGES\tLAST ACTIVE\tTITLE")

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
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			task.Name, task.Status, task.Stage, progress, changes, task.LastActive, title)
	}

	if t.Footer != "" {
		fmt.Fprintf(w, "\t\t\t\t%s\n", t.Footer)
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

	// Headers (no TITLE - it goes on second line)
	headers := []string{"TASK", "STATUS", "STAGE", "PROGRESS", "CHANGES", "LAST ACTIVE"}

	// Calculate column widths
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, task := range t.Tasks {
		row := []string{
			task.Name,
			task.Status,
			task.Stage,
			formatProgressBar(task.Progress),
			formatChangesForTask(task, false), // Use plain for width calculation
			task.LastActive,
		}
		for i, cell := range row {
			// For progress bar and changes, use display width not byte length
			cellWidth := ansi.StringWidth(cell)
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
			padRight(colorStatusByEnum(task.UserStatus, task.Status), widths[1]),
			padRight(task.Stage, widths[2]),
			padRight(formatProgressBar(task.Progress), widths[3]),
			padRight(formatChangesForTask(task, true), widths[4]),
			padRight(task.LastActive, widths[5]),
		}
		lines = append(lines, strings.Join(cells, "  "))

		// Title row (dimmed, aligned with task name).
		// Truncate to terminal width so the box stays inside the viewport:
		// styleBox uses 1 left margin + 2 border + 2 padding = 5 horizontal
		// overhead; "└ " prefix adds 2. Reserve 1 extra cell as a safety
		// margin against wide chars or rounding.
		if task.Title != "" {
			title := task.Title
			if task.HasReview {
				title += "  [reviewed]"
			}
			if t.TerminalWidth > 0 {
				maxTitle := t.TerminalWidth - 8
				if maxTitle > 0 {
					title = ansi.Truncate(title, maxTitle, "…")
				}
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

// colorStatusByEnum colors the display text by the carried UserStatus enum
// value rather than re-parsing the rendered text. The enum is the same one that
// produced the text (task.UserStatusFor), so color and text cannot drift. The
// values mirror task.UserStatus; the producer passes string(task.UserStatusFor(...)).
//
// render.Status (text.go) is a separate, deliberately string-keyed colorizer for
// non-task tokens (commit/merged log markers, review outcomes) and is not unified
// here — it has a different domain.
func colorStatusByEnum(userStatus, text string) string {
	switch userStatus {
	case "working":
		return styleStatusWorking.Render(text)
	case "replied":
		return styleStatusReplied.Render(text)
	case "error":
		return styleStatusError.Render(text) // also covers "interrupted" (same enum)
	case "merged":
		return styleStatusMerged.Render(text)
	case "closed":
		return styleStatusClosed.Render(text)
	case "draft":
		return styleStatusDraft.Render(text)
	default:
		s := strings.TrimSpace(text)
		if s == "" || s == "-" || s == "—" {
			return styleDim.Render("—")
		}
		return styleDim.Render(text)
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
