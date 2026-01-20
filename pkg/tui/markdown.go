package tui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

var (
	mdMu    sync.Mutex
	mdWidth int
	mdTheme string
	mdR     *glamour.TermRenderer
)

func renderMarkdown(width int, s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	r := markdownRenderer(width)
	if r == nil {
		return s
	}
	out, err := r.Render(s)
	if err != nil {
		return s
	}
	// Strip glamour's default left margin
	out = stripLeftMargin(out, 10)
	return strings.Trim(out, "\n")
}

// stripLeftMargin removes up to n leading spaces from each line.
func stripLeftMargin(s string, n int) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		spaces := 0
		for spaces < n && spaces < len(line) && line[spaces] == ' ' {
			spaces++
		}
		lines[i] = line[spaces:]
	}
	return strings.Join(lines, "\n")
}

func markdownRenderer(width int) *glamour.TermRenderer {
	if width <= 0 {
		width = 80
	}

	// Use lipgloss's cached detection (same as AdaptiveColor uses)
	theme := "dark"
	if !lipgloss.HasDarkBackground() {
		theme = "light"
	}

	mdMu.Lock()
	defer mdMu.Unlock()

	if mdR != nil && mdWidth == width && mdTheme == theme {
		return mdR
	}

	// Get base style and set document margin to 0
	var style ansi.StyleConfig
	if theme == "dark" {
		style = styles.DarkStyleConfig
	} else {
		style = styles.LightStyleConfig
	}
	style.Document.Margin = uintPtr(0)

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		mdR = nil
		mdWidth = 0
		mdTheme = ""
		return nil
	}

	mdR = r
	mdWidth = width
	mdTheme = theme
	return mdR
}

func uintPtr(v uint) *uint {
	return &v
}
