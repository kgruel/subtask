package logs

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	styleTime      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleUser      = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	styleAgent     = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	styleReasoning = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	styleTool      = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleToolOut   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleError     = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	styleExitOK    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleExitFail  = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// Formatter formats log entries for display.
type Formatter struct {
	Pretty     bool      // Use colors and styling
	Timestamps bool      // Show timestamps
	NoTrunc    bool      // Don't truncate output
	StartTime  time.Time // Reference time for relative timestamps
}

// Format formats a single log entry.
func (f *Formatter) Format(e LogEntry) string {
	var parts []string

	// Timestamp
	if f.Timestamps {
		ts := f.formatTime(e.Time)
		if f.Pretty {
			ts = styleTime.Render(ts)
		}
		parts = append(parts, ts)
	}

	// Truncate summary unless NoTrunc is set
	summary := e.Summary
	if !f.NoTrunc {
		summary = f.truncate(summary, 200)
	}

	// Kind-specific formatting
	var content string
	switch e.Kind {
	case KindUserMessage:
		prefix := "USER:"
		if f.Pretty {
			prefix = styleUser.Render(prefix)
		}
		content = prefix + " " + summary

	case KindAgentMessage:
		prefix := "AGENT:"
		if f.Pretty {
			prefix = styleAgent.Render(prefix)
		}
		content = prefix + " " + summary

	case KindReasoning:
		prefix := "THINKING:"
		text := summary
		if f.Pretty {
			prefix = styleReasoning.Render(prefix)
			text = styleReasoning.Render(text)
		}
		content = prefix + " " + text

	case KindToolCall:
		prefix := "→"
		if f.Pretty {
			prefix = styleTool.Render(prefix)
		}
		content = prefix + " " + summary

	case KindToolOutput:
		// Skip tool outputs with no useful content and no exit code
		if e.Summary == "" && e.ExitCode == nil {
			return "" // Will be filtered out
		}

		prefix := "←"
		exitStr := ""
		if e.ExitCode != nil {
			if *e.ExitCode == 0 {
				exitStr = "[0]"
				if f.Pretty {
					exitStr = styleExitOK.Render(exitStr)
				}
			} else {
				exitStr = fmt.Sprintf("[%d]", *e.ExitCode)
				if f.Pretty {
					exitStr = styleExitFail.Render(exitStr)
				}
			}
		}

		text := summary
		if f.Pretty {
			prefix = styleToolOut.Render(prefix)
			if text != "" {
				text = styleToolOut.Render(text)
			}
		}

		// Build output: "← text [exit]" or "← [exit]" or "← text"
		if text != "" && exitStr != "" {
			content = prefix + " " + text + " " + exitStr
		} else if text != "" {
			content = prefix + " " + text
		} else {
			content = prefix + " " + exitStr
		}

	case KindError:
		prefix := "ERROR:"
		if f.Pretty {
			prefix = styleError.Render(prefix)
			content = prefix + " " + styleError.Render(e.Summary)
		} else {
			content = prefix + " " + e.Summary
		}

	case KindSessionStart:
		content = "--- Session started ---"
		if f.Pretty {
			content = styleTime.Render(content)
		}

	default:
		content = summary
	}

	parts = append(parts, content)
	return strings.Join(parts, " ")
}

// truncate shortens a string to max length, collapsing whitespace.
func (f *Formatter) truncate(s string, max int) string {
	// Collapse whitespace
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func (f *Formatter) formatTime(t time.Time) string {
	if t.IsZero() {
		return "          "
	}

	// If we have a reference start time and it's the same day, show relative
	if !f.StartTime.IsZero() {
		// Show as HH:MM:SS if same day
		if t.Year() == f.StartTime.Year() && t.YearDay() == f.StartTime.YearDay() {
			return t.Format("15:04:05")
		}
	}

	// Full timestamp
	return t.Format("15:04:05")
}

// FormatSessionHeader formats session info as a header.
func (f *Formatter) FormatSessionHeader(info *SessionInfo) string {
	if info == nil {
		return ""
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("Session: %s", info.ID))

	if info.Model != "" {
		parts = append(parts, fmt.Sprintf("Model: %s", info.Model))
	}
	if info.CWD != "" {
		parts = append(parts, fmt.Sprintf("CWD: %s", info.CWD))
	}

	header := strings.Join(parts, " | ")
	if f.Pretty {
		return styleTime.Render(header)
	}
	return header
}
