package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/mattn/go-runewidth"
	"github.com/kgruel/subtask/pkg/diffparse"
)

func renderDiffView(m model, leftPad string) string {
	if m.diffErr != nil && m.diffTaskName == m.selectedTaskName {
		return leftPad + styleStatusError.Render(m.diffErr.Error())
	}
	if m.diffTaskName != m.selectedTaskName {
		return leftPad + styleDim.Render("Loading changes...")
	}

	// Use pre-calculated widths from resize() to ensure consistency
	leftW := m.diffSidebarW
	rightW := m.diffViewWidth

	leftContent := renderChangesTreePane(m, leftW)
	rightContent := renderChangesDiffPane(m, rightW)
	leftContent = markChangesPaneBody(m, leftContent, leftW, zoneDiffFilesPane())
	rightContent = markChangesPaneBody(m, rightContent, rightW, zoneDiffCodePane())
	joined := joinChangesPanes(leftContent, rightContent, leftW, rightW)
	return addPadding(joined, leftPad)
}

func markChangesPaneBody(m model, content string, width int, paneID string) string {
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return content
	}
	body := strings.Join(lines[2:], "\n")
	body = zone.Mark(paneID, body)
	out := append([]string{}, lines[:2]...)
	out = append(out, strings.Split(body, "\n")...)
	return strings.Join(out, "\n")
}

func joinChangesPanes(leftContent, rightContent string, leftW, rightW int) string {
	leftLines := strings.Split(leftContent, "\n")
	rightLines := strings.Split(rightContent, "\n")

	maxLines := max(len(leftLines), len(rightLines))
	for len(leftLines) < maxLines {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < maxLines {
		rightLines = append(rightLines, "")
	}

	leftStyle := lipgloss.NewStyle().Width(leftW)
	rightStyle := lipgloss.NewStyle().Width(rightW)
	sep := styleChangesSep.Render

	// Box drawing
	leftRule := strings.Repeat("─", leftW)
	rightRule := strings.Repeat("─", rightW)

	var out []string

	// Top border: ┌───┬───┐
	out = append(out, sep("┌")+sep(leftRule)+sep("┬")+sep(rightRule)+sep("┐"))

	// Content rows (skip header lines 0-1, start from line 2)
	for i := 2; i < maxLines; i++ {
		out = append(out, sep("│")+leftStyle.Render(leftLines[i])+sep("│")+rightStyle.Render(rightLines[i])+sep("│"))
	}

	// Bottom border: └───┴───┘
	out = append(out, sep("└")+sep(leftRule)+sep("┴")+sep(rightRule)+sep("┘"))

	return strings.Join(out, "\n")
}

func renderChangesTreePane(m model, width int) string {
	// Summary line
	summary := fmt.Sprintf("Files (%d)", len(m.diffFiles))
	rule := styleChangesHunk.Render(strings.Repeat("─", max(0, width)))

	lines := []string{
		styleBold.Render(ansi.Truncate(summary, max(0, width), "")),
		rule,
	}

	if m.diffSearchActive {
		searchBg := lipgloss.AdaptiveColor{Light: "254", Dark: "238"}
		searchBgStyle := lipgloss.NewStyle().Background(searchBg)
		lines = append(lines, searchBgStyle.Width(width).Render(renderChangesSearchBox(m, width, searchBg)))
	}

	if len(m.diffFiles) == 0 {
		lines = append(lines, styleDim.Render("(no changes)"))
		return strings.Join(lines, "\n")
	}
	if len(m.diffFilteredPaths) == 0 {
		lines = append(lines,
			styleDim.Render(center(width, "No matching files.")),
			styleDim.Render(center(width, "Press Esc to clear search")),
		)
		return strings.Join(lines, "\n")
	}

	if m.diffTreeLines == nil {
		lines = append(lines, styleDim.Render(center(width, "Loading...")))
		return strings.Join(lines, "\n")
	}

	treeLines := m.diffTreeLines

	headerLines := changesTreeHeaderLines
	if m.diffSearchActive {
		headerLines += changesSearchHeaderExtra
	}
	visible := max(0, m.diffViewHeight-headerLines)
	start := clampInt(m.diffTreeScroll, 0, max(0, len(treeLines)))
	end := min(len(treeLines), start+visible)

	lines = append(lines, treeLines[start:end]...)
	return strings.Join(lines, "\n")
}

func renderChangesSearchBox(m model, maxWidth int, bg lipgloss.TerminalColor) string {
	if maxWidth < 10 {
		maxWidth = 10
	}
	bgStyle := lipgloss.NewStyle().Background(bg)
	prefix := styleDim.Background(bg).Render("/")
	value := m.diffSearchInput.Value()
	if len(value) > maxWidth-5 {
		value = value[:maxWidth-5]
	}
	cursor := ""
	if m.diffSearchActive {
		cursor = "█"
	}
	if value == "" && !m.diffSearchActive {
		placeholder := styleDim.Background(bg).Render("filter files...")
		return prefix + bgStyle.Render(" ") + placeholder
	}
	return prefix + bgStyle.Render(" "+value+cursor)
}

func renderChangesDiffPane(m model, width int) string {
	header := " " + styleBold.Render("Changes")
	if m.diffHasCurrent {
		header = " " + styleBold.Render(m.diffCurrentPath)
		header += styleDim.Render("  ")
		if m.diffSideBySide {
			header += styleDim.Render("side-by-side")
		} else {
			header += styleDim.Render("unified")
		}
	}
	header = ansi.Truncate(header, max(0, width), "")
	rule := styleChangesHunk.Render(strings.Repeat("─", max(0, width)))

	lines := []string{header, rule}

	if !m.diffHasCurrent {
		lines = append(lines, styleDim.Render(center(width, "(no file selected)")))
		return strings.Join(lines, "\n")
	}
	if m.diffCurrentFile.Binary {
		lines = append(lines, styleDim.Render(center(width, "Binary file; diff not shown.")))
		return strings.Join(lines, "\n")
	}
	if m.diffLoading {
		lines = append(lines, styleDim.Render(center(width, "Loading diff...")))
		return strings.Join(lines, "\n")
	}
	if m.diffDoc == nil {
		lines = append(lines, styleDim.Render(center(width, "(no diff)")))
		return strings.Join(lines, "\n")
	}
	if m.diffSideBySide && len(m.diffDoc.SideBySide) == 0 {
		lines = append(lines, styleDim.Render(center(width, "(no diff)")))
		return strings.Join(lines, "\n")
	}
	if !m.diffSideBySide && len(m.diffDoc.Unified) == 0 {
		lines = append(lines, styleDim.Render(center(width, "(no diff)")))
		return strings.Join(lines, "\n")
	}

	contentHeight := m.diffContentHeight()
	totalLines := m.diffLayoutTotalLines
	if totalLines <= 0 {
		if m.diffSideBySide {
			totalLines = len(m.diffDoc.SideBySide)
		} else {
			totalLines = len(m.diffDoc.Unified)
		}
	}
	start := clampInt(m.diffScrollY, 0, max(0, totalLines))
	end := min(totalLines, start+contentHeight)

	if m.diffSideBySide {
		oldW := max(4, digits(m.diffDoc.OldMaxLine))
		newW := max(4, digits(m.diffDoc.NewMaxLine))
		for i := start; i < end; i++ {
			lines = append(lines, renderSideRow(m.diffDoc.SideBySide[i], width, oldW, newW))
		}
		return strings.Join(lines, "\n")
	}

	lineNoW := m.diffLayoutLineNoW
	wrapW := m.diffLayoutUnifiedWrap
	prefix := m.diffLayoutPrefix
	if lineNoW == 0 {
		maxLine := max(m.diffDoc.UnifiedOld, m.diffDoc.UnifiedNew)
		lineNoW = max(4, digits(maxLine))
	}
	if prefix == nil {
		// Defensive fallback: render without wrapping.
		wrapW = 0
	}

	rowIdx, sub := unifiedLocate(prefix, start)
	remaining := contentHeight
	for rowIdx < len(m.diffDoc.Unified) && remaining > 0 {
		row := m.diffDoc.Unified[rowIdx]
		rowLines := renderUnifiedRowLines(row, width, lineNoW, wrapW, sub, remaining)
		lines = append(lines, rowLines...)
		remaining -= len(rowLines)
		rowIdx++
		sub = 0
	}
	return strings.Join(lines, "\n")
}

func renderUnifiedRowLines(r diffparse.UnifiedRow, width, lineNoW, wrapW, startSub, maxLines int) []string {
	if startSub < 0 {
		startSub = 0
	}
	if maxLines <= 0 {
		return nil
	}

	if r.Kind == diffparse.KindSeparator {
		if startSub > 0 {
			return nil
		}
		return []string{styleChangesHunk.Render(strings.Repeat("─", max(0, width)))}
	}

	// Show only one line number (new file line, or old for deletions)
	lineNo := r.NewLine
	if lineNo <= 0 {
		lineNo = r.OldLine
	}
	lineNoStr := ""
	if lineNo > 0 {
		lineNoStr = strconv.Itoa(lineNo)
	}

	gutterW := lineNoW + 1
	codeW := max(0, width-gutterW)

	prefixChar := " "
	switch r.Kind {
	case diffparse.KindAdd:
		prefixChar = "+"
	case diffparse.KindDelete:
		prefixChar = "-"
	}

	codeStyle := lipgloss.NewStyle().Width(codeW)
	switch r.Kind {
	case diffparse.KindAdd:
		codeStyle = styleChangesPlus.Width(codeW)
	case diffparse.KindDelete:
		codeStyle = styleChangesMinus.Width(codeW)
	}

	// Very narrow terminals: degrade to a single truncated line.
	if wrapW <= 0 || codeW <= 2 {
		if startSub > 0 {
			return nil
		}
		gutter := styleChangesGutter.Render(fmt.Sprintf("%*s ", lineNoW, lineNoStr))
		code := ansi.Truncate(prefixChar+" "+normalizeDiffText(r.Text), max(0, codeW), "")
		return []string{gutter + codeStyle.Render(code)}
	}

	segments := wrapCells(normalizeDiffText(r.Text), wrapW)
	if len(segments) == 0 {
		segments = []string{""}
	}

	var out []string
	for i := startSub; i < len(segments) && len(out) < maxLines; i++ {
		ln := ""
		if i == 0 {
			ln = lineNoStr
		}
		gutter := styleChangesGutter.Render(fmt.Sprintf("%*s ", lineNoW, ln))

		lead := "  "
		if i == 0 {
			lead = prefixChar + " "
		}
		code := lead + segments[i]
		code = ansi.Truncate(code, max(0, codeW), "")
		out = append(out, gutter+codeStyle.Render(code))
	}
	return out
}

func unifiedLocate(prefix []int, line int) (rowIdx, sub int) {
	if len(prefix) < 2 {
		return 0, 0
	}
	if line < 0 {
		line = 0
	}
	if line >= prefix[len(prefix)-1] {
		line = prefix[len(prefix)-1] - 1
		if line < 0 {
			line = 0
		}
	}

	// Find i where prefix[i] <= line < prefix[i+1].
	i := sort.Search(len(prefix)-1, func(i int) bool { return prefix[i+1] > line })
	if i < 0 {
		i = 0
	}
	if i >= len(prefix)-1 {
		i = len(prefix) - 2
	}
	return i, line - prefix[i]
}

func normalizeDiffText(s string) string {
	// Tab expansion keeps wrapping predictable across terminals.
	return strings.ReplaceAll(s, "\t", "    ")
}

func wrapCells(s string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	if s == "" {
		return []string{""}
	}

	var (
		out []string
		b   strings.Builder
		w   int
	)

	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if rw <= 0 {
			rw = 1
		}
		if w+rw > width && w > 0 {
			out = append(out, b.String())
			b.Reset()
			w = 0
		}
		b.WriteRune(r)
		w += rw
	}

	out = append(out, b.String())
	return out
}

func renderSideRow(r diffparse.SideBySideRow, width, oldW, newW int) string {
	if r.Kind == diffparse.KindSeparator {
		return styleChangesHunk.Render(strings.Repeat("─", max(0, width)))
	}

	leftGutter := formatLineNo(r.OldLine, oldW)
	rightGutter := formatLineNo(r.NewLine, newW)
	leftGutter = styleChangesGutter.Render(leftGutter + " ")
	rightGutter = styleChangesGutter.Render(rightGutter + " ")
	sep := styleChangesSep.Render("│")

	fixed := ansi.StringWidth(leftGutter) + ansi.StringWidth(sep) + ansi.StringWidth(rightGutter)
	codeTotal := max(0, width-fixed)
	leftCodeW := codeTotal / 2
	rightCodeW := codeTotal - leftCodeW

	leftText := ansi.Truncate(r.OldText, leftCodeW, "")
	rightText := ansi.Truncate(r.NewText, rightCodeW, "")

	leftTextStyle := lipgloss.NewStyle().Width(leftCodeW)
	rightTextStyle := lipgloss.NewStyle().Width(rightCodeW)
	switch r.Kind {
	case diffparse.KindDelete:
		leftTextStyle = styleChangesMinus.Width(leftCodeW)
	case diffparse.KindAdd:
		rightTextStyle = styleChangesPlus.Width(rightCodeW)
	case diffparse.KindModify:
		leftTextStyle = styleChangesMinus.Width(leftCodeW)
		rightTextStyle = styleChangesPlus.Width(rightCodeW)
	}

	return leftGutter + leftTextStyle.Render(leftText) + sep + rightGutter + rightTextStyle.Render(rightText)
}

func digits(n int) int {
	if n <= 0 {
		return 1
	}
	return len(strconv.Itoa(n))
}

func formatLineNo(n int, width int) string {
	if n <= 0 {
		return strings.Repeat(" ", width)
	}
	s := strconv.Itoa(n)
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}
