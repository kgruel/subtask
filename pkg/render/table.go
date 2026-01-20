package render

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

// Table renders a table with headers and rows.
type Table struct {
	Headers []string
	Rows    [][]string
	Footer  string // Optional footer line (shown below table)
}

// RenderPlain renders the table as plain tab-separated text.
func (t *Table) RenderPlain() string {
	var buf strings.Builder
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	// Headers
	fmt.Fprintln(w, strings.Join(t.Headers, "\t"))

	// Rows
	for _, row := range t.Rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	// Footer
	if t.Footer != "" {
		// Pad with empty columns to align with last column
		padding := make([]string, len(t.Headers)-1)
		fmt.Fprintf(w, "%s\t%s\n", strings.Join(padding, "\t"), t.Footer)
	}

	w.Flush()
	return buf.String()
}

// RenderPretty renders the table with a box border.
func (t *Table) RenderPretty() string {
	if len(t.Headers) == 0 {
		return ""
	}

	// Calculate column widths
	widths := make([]int, len(t.Headers))
	for i, h := range t.Headers {
		widths[i] = len(h)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Build content
	var lines []string

	// Header row
	headerCells := make([]string, len(t.Headers))
	for i, h := range t.Headers {
		headerCells[i] = styleTableHeader.Render(padRight(h, widths[i]))
	}
	lines = append(lines, strings.Join(headerCells, "  "))

	// Separator
	sepParts := make([]string, len(widths))
	for i, w := range widths {
		sepParts[i] = strings.Repeat("─", w)
	}
	lines = append(lines, styleDim.Render(strings.Join(sepParts, "──")))

	// Data rows
	for _, row := range t.Rows {
		cells := make([]string, len(t.Headers))
		for i := range t.Headers {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			// Apply status coloring
			if i == 1 { // STATUS column
				val = colorStatus(val)
			}
			cells[i] = padRight(val, widths[i])
		}
		lines = append(lines, strings.Join(cells, "  "))
	}

	content := strings.Join(lines, "\n")
	result := styleBox.Render(content)

	// Footer outside box
	if t.Footer != "" {
		result += "\n" + styleDim.Render(strings.Repeat(" ", 40)+t.Footer)
	}

	return result + "\n"
}

// Print renders and prints the table.
func (t *Table) Print() {
	if Pretty {
		fmt.Print(t.RenderPretty())
	} else {
		fmt.Print(t.RenderPlain())
	}
}

// padRight pads a string to the given width.
func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// colorStatus applies color to status values (pretty mode only).
func colorStatus(status string) string {
	// Extract base status (without timing info)
	base := status
	if idx := strings.Index(status, " ("); idx > 0 {
		base = status[:idx]
	}

	switch base {
	case "open":
		return styleStatusWorking.Render(status)
	case "working":
		return styleStatusWorking.Render(status)
	case "running":
		return styleStatusWorking.Render(status)
	case "replied":
		return styleStatusReplied.Render(status)
	case "error":
		return styleStatusError.Render(status)
	case "closed":
		return styleStatusClosed.Render(status)
	case "merged", "✓ merged":
		return styleStatusMerged.Render(status)
	default:
		return status
	}
}

// PrintTable is a convenience function to print a table directly.
func PrintTable(headers []string, rows [][]string, footer string) {
	t := &Table{Headers: headers, Rows: rows, Footer: footer}
	t.Print()
}

// SimpleTable prints a minimal table for quick output.
// In pretty mode, just adds light styling. No box.
func SimpleTable(headers []string, rows [][]string) {
	var buf strings.Builder
	w := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)

	if Pretty {
		// Styled headers
		styledHeaders := make([]string, len(headers))
		for i, h := range headers {
			styledHeaders[i] = styleTableHeader.Render(h)
		}
		fmt.Fprintln(w, strings.Join(styledHeaders, "\t"))
	} else {
		fmt.Fprintln(w, strings.Join(headers, "\t"))
	}

	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	w.Flush()
	fmt.Print(buf.String())
}

// progressBar renders a progress bar. Returns "███░░ 3/5" style string.
func progressBar(done, total int, width int) string {
	if total == 0 {
		return "-"
	}

	filled := (done * width) / total
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	text := fmt.Sprintf("%d/%d", done, total)

	if Pretty {
		return styleSuccess.Render(bar) + " " + text
	}
	return text
}

// ProgressBar returns a formatted progress string.
// In plain mode: "3/5"
// In pretty mode: "███░░ 3/5"
func ProgressBar(done, total int) string {
	if !Pretty {
		if total == 0 {
			return "-"
		}
		return fmt.Sprintf("%d/%d", done, total)
	}
	return progressBar(done, total, 5)
}

// lipgloss table alternative using lipgloss.Table (if available)
// For now, we use manual rendering above for more control.

// Write is a helper for writing styled output to a writer.
func Write(w *os.File, s string) {
	fmt.Fprint(w, s)
}
