package tui

import (
	"fmt"
	"strings"

	"github.com/kgruel/subtask/pkg/render"
)

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
