package tui

import (
	"fmt"
	"strings"

	"github.com/kgruel/subtask/pkg/render"
)

func isBinary(b []byte) bool {
	limit := len(b)
	if limit > 8192 {
		limit = 8192
	}
	for i := 0; i < limit; i++ {
		if b[i] == 0 {
			return true
		}
	}
	return false
}

func renderArtifactsViewContent(m model) string {
	if len(m.artifacts) == 0 {
		return ""
	}
	a := m.artifacts[m.artifactSelected]
	sizeStr := render.FormatArtifactSize(a.Size)
	header := styleBold.Render(a.Name) + styleDim.Render("  ·  "+a.Kind+"  ·  "+sizeStr)

	key := artifactCacheKey(m.selectedTaskName, a.Path)
	var body string
	switch {
	case m.artifactBinary[key]:
		body = styleDim.Render(fmt.Sprintf("Binary content (%d bytes)", a.Size))
	case m.artifactMissing[key]:
		body = styleDim.Render("File no longer exists on disk")
	default:
		content, cached := m.artifactContent[key]
		if !cached {
			body = styleDim.Render("Loading...")
		} else if strings.HasSuffix(a.Name, ".md") {
			body = renderMarkdown(m.vpArtifactView.Width-2, content)
		} else {
			body = content
		}
	}

	return header + "\n\n" + body
}

func renderArtifactsList(m model) string {
	var lines []string

	// Header
	if len(m.artifacts) == 0 {
		lines = append(lines, styleBold.Render("Artifacts")+styleDim.Render(" (none)"))
	} else {
		lines = append(lines, styleBold.Render("Artifacts")+styleDim.Render(fmt.Sprintf(" (%d)", len(m.artifacts))))
	}
	lines = append(lines, "")

	if m.artifactsErr != nil {
		lines = append(lines, styleStatusError.Render(m.artifactsErr.Error()))
		return strings.Join(lines, "\n")
	}

	if len(m.artifacts) == 0 {
		lines = append(lines, styleDim.Render("(no artifacts yet)"))
		return strings.Join(lines, "\n")
	}

	for i, a := range m.artifacts {
		var bullet string
		if i == m.artifactSelected {
			bullet = "▶ "
		} else {
			bullet = "  "
		}

		var sizeStr string
		if a.Missing {
			sizeStr = styleDim.Render("—")
		} else {
			sizeStr = render.FormatArtifactSize(a.Size)
		}

		name := a.Name
		if a.Missing {
			name += styleDim.Render(" (missing)")
		}

		line := bullet + name + "  " + styleDim.Render(a.Kind) + "  " + sizeStr
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}
