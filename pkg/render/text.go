package render

import (
	"fmt"
	"os"
	"strings"
)

// Message helpers - these print immediately

// Success prints a success message with checkmark.
func Success(msg string) {
	if Pretty {
		fmt.Printf("%s %s\n", styleSuccess.Render("✓"), msg)
	} else {
		fmt.Printf("✓ %s\n", msg)
	}
}

// Info prints an informational message.
func Info(msg string) {
	fmt.Println(msg)
}

// Warning prints a warning message to stderr.
func Warning(msg string) {
	if Pretty {
		fmt.Fprintf(os.Stderr, "%s %s\n", styleWarning.Render("warning:"), msg)
	} else {
		fmt.Fprintf(os.Stderr, "warning: %s\n", msg)
	}
}

// Error prints an error message to stderr.
func Error(msg string) {
	if Pretty {
		fmt.Fprintf(os.Stderr, "%s %s\n", styleError.Render("error:"), msg)
	} else {
		fmt.Fprintf(os.Stderr, "error: %s\n", msg)
	}
}

// Section prints a section header.
// Plain:  "\n\n─── Title ───\n"
// Pretty: styled with color
func Section(title string) {
	if Pretty {
		line := strings.Repeat("─", 3)
		fmt.Printf("\n\n%s %s %s\n", styleDim.Render(line), styleHighlight.Render(title), styleDim.Render(line))
	} else {
		fmt.Printf("\n\n─── %s ───\n", title)
	}
}

// SectionContent prints content under a section (trimmed).
func SectionContent(content string) {
	content = strings.TrimSpace(content)
	if content != "" {
		fmt.Println(content)
	}
}

// KV represents a key-value pair for display.
type KV struct {
	Key   string
	Value string
}

// KeyValue prints a list of key-value pairs.
// Plain:  "Key: value\n"
// Pretty: aligned with styled keys
type KeyValueList struct {
	Pairs  []KV
	Indent int  // spaces to indent (default 0)
	InBox  bool // wrap in a box (pretty mode only)
}

// RenderPlain renders key-value pairs as plain text.
func (kv *KeyValueList) RenderPlain() string {
	var buf strings.Builder
	indent := strings.Repeat(" ", kv.Indent)
	for _, p := range kv.Pairs {
		fmt.Fprintf(&buf, "%s%s: %s\n", indent, p.Key, p.Value)
	}
	return buf.String()
}

// RenderPretty renders key-value pairs with styling.
func (kv *KeyValueList) RenderPretty() string {
	if len(kv.Pairs) == 0 {
		return ""
	}

	// Find max key length for alignment
	maxKey := 0
	for _, p := range kv.Pairs {
		if len(p.Key) > maxKey {
			maxKey = len(p.Key)
		}
	}

	var lines []string
	for _, p := range kv.Pairs {
		key := styleBold.Render(padRight(p.Key, maxKey))
		lines = append(lines, fmt.Sprintf("  %s  %s", key, p.Value))
	}

	content := strings.Join(lines, "\n")

	if kv.InBox {
		return styleBox.Render(content) + "\n"
	}
	return content + "\n"
}

// Print renders and prints the key-value list.
func (kv *KeyValueList) Print() {
	if Pretty {
		fmt.Print(kv.RenderPretty())
	} else {
		fmt.Print(kv.RenderPlain())
	}
}

// Divider prints a horizontal divider line.
func Divider() {
	if Pretty {
		fmt.Println(styleDim.Render(strings.Repeat("─", 40)))
	} else {
		fmt.Println(strings.Repeat("-", 40))
	}
}

// Bold returns text in bold (pretty mode) or unchanged (plain mode).
func Bold(s string) string {
	if Pretty {
		return styleBold.Render(s)
	}
	return s
}

// Dim returns dimmed text (pretty mode) or unchanged (plain mode).
func Dim(s string) string {
	if Pretty {
		return styleDim.Render(s)
	}
	return s
}

// Highlight returns highlighted text (pretty mode) or unchanged (plain mode).
func Highlight(s string) string {
	if Pretty {
		return styleHighlight.Render(s)
	}
	return s
}

// DiagramEdge is a single routing edge for a flow note line.
type DiagramEdge struct {
	Label    string // option name or branch field
	Target   string // destination step ID
	Loopback bool   // true when Target is earlier in the chain
}

// DiagramStep carries all display data for one step in FormatRoutineDiagram.
// Callers build this from *routine.Routine using RoutineToDiagramSteps.
type DiagramStep struct {
	ID       string
	Terminal bool
	Gate     bool
	// Edges holds conditional edges: branches for regular steps, options for gates.
	Edges []DiagramEdge
}

// FormatRoutineDiagram renders a routine's step chain with structural markers.
//
// Plain:  "(doing) → review → ready!"
// Pretty: current step highlighted; gate/branch sigils colored.
//
// Sigils appended to step names:
//
//	! terminal   * gate   ? branch step (has conditional branches:)
//
// When any step has branches or gate options, a compact flow note is appended
// below the chain listing each non-trivial step's edges. Loopback edges
// (target earlier in the chain) are prefixed with ↩ instead of →.
//
// Use RoutineToDiagramSteps to convert a *routine.Routine to []DiagramStep.
func FormatRoutineDiagram(steps []DiagramStep, current string) string {
	if len(steps) == 0 {
		return ""
	}

	// Build main chain with type sigils.
	parts := make([]string, len(steps))
	for i, s := range steps {
		sigil := diagramSigil(s)
		if s.ID == current && current != "" {
			if Pretty {
				parts[i] = styleHighlight.Bold(true).Render(s.ID) + sigil
			} else {
				parts[i] = "(" + s.ID + ")" + sigil
			}
		} else {
			parts[i] = s.ID + sigil
		}
	}
	chain := strings.Join(parts, " → ")

	// Build flow notes for steps with edges.
	var notes []string
	for _, s := range steps {
		if len(s.Edges) == 0 {
			continue
		}
		edgeParts := make([]string, len(s.Edges))
		for j, e := range s.Edges {
			if e.Loopback {
				edgeParts[j] = e.Label + " ↩ " + e.Target
			} else {
				edgeParts[j] = e.Label + " → " + e.Target
			}
		}
		var prefix string
		if s.Gate {
			prefix = "  * "
			if Pretty {
				prefix = "  " + styleWarning.Render("*") + " "
			}
		} else {
			prefix = "  ? "
			if Pretty {
				prefix = "  " + styleHighlight.Render("?") + " "
			}
		}
		notes = append(notes, prefix+s.ID+": "+strings.Join(edgeParts, " | "))
	}

	if len(notes) == 0 {
		return chain
	}
	return chain + "\n" + strings.Join(notes, "\n")
}

// diagramSigil returns the type marker for a step: "!" terminal, "*" gate,
// "?" branch step, "" regular.
func diagramSigil(s DiagramStep) string {
	switch {
	case s.Terminal:
		return "!"
	case s.Gate:
		return "*"
	case len(s.Edges) > 0:
		return "?"
	default:
		return ""
	}
}

// Status is a general-purpose colorizer for already-rendered status-like tokens
// (e.g. "commit"/"merged" log markers, review outcomes such as "success"/"blocked").
// It is intentionally string-keyed and is NOT a task.UserStatus mapper — the task
// list colors by enum via colorStatusByEnum (tasklist.go). The two have different
// domains and are deliberately not unified.
func Status(status string) string {
	if !Pretty {
		return status
	}

	base := status
	if idx := strings.Index(status, " ("); idx > 0 {
		base = status[:idx]
	}

	switch base {
	case "open":
		return styleStatusWorking.Render(status)
	case "merged", "✓ merged", "commit":
		return styleStatusMerged.Render(status)
	case "closed":
		return styleStatusClosed.Render(status)
	case "working":
		return styleStatusWorking.Render(status)
	case "running":
		return styleStatusWorking.Render(status)
	case "replied":
		return styleStatusReplied.Render(status)
	case "error", "blocked", "interrupted":
		return styleStatusError.Render(status)
	case "success":
		return styleStatusWorking.Render(status)
	default:
		return status
	}
}
