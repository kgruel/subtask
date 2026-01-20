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

// PrintKV is a convenience function to print key-value pairs.
func PrintKV(pairs ...KV) {
	kv := &KeyValueList{Pairs: pairs}
	kv.Print()
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

// FormatStageProgression formats stage names with current highlighted.
// Plain: "plan → (implement) → review"
// Pretty: current stage rendered with highlight + bold.
func FormatStageProgression(stages []string, current string) string {
	if len(stages) == 0 {
		return ""
	}

	parts := make([]string, len(stages))
	for i, name := range stages {
		if name == current && current != "" {
			if Pretty {
				parts[i] = styleHighlight.Bold(true).Render(name)
			} else {
				parts[i] = "(" + name + ")"
			}
		} else {
			parts[i] = name
		}
	}

	return strings.Join(parts, " → ")
}

// Status returns a styled status string.
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
	case "merged", "✓ merged":
		return styleStatusMerged.Render(status)
	case "closed":
		return styleStatusClosed.Render(status)
	case "working":
		return styleStatusWorking.Render(status)
	case "running":
		return styleStatusWorking.Render(status)
	case "replied":
		return styleStatusReplied.Render(status)
	case "error":
		return styleStatusError.Render(status)
	default:
		return status
	}
}
