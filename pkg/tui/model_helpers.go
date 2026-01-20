package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/zippoxer/subtask/pkg/task"
)

const (
	listHeaderLines   = 2 // title + blank
	listFooterLines   = 2 // status line + footer
	detailHeaderLines = 3 // task strip + tabs + blank
	detailFooterLines = 2 // status line + footer
)

func (m *model) resize() {
	// List view scrolling.
	m.ensureSelectionVisible()

	// Detail viewports.
	// Header: 3 lines + blank: 1, Tab bar: 1 + blank: 1, footer: 2 = 8
	contentHeight := max(0, m.height-8)
	leftPad := 2                              // "  " prefix used in detail view
	contentWidth := max(0, m.width-leftPad-2) // Match header: m.width - len(leftPad) - 2

	m.vpOverview.Width = contentWidth
	m.vpOverview.Height = contentHeight
	m.vpConversation.Width = contentWidth
	m.vpConversation.Height = contentHeight
	m.vpConflicts.Width = contentWidth
	m.vpConflicts.Height = contentHeight

	leftW, rightW := diffPaneWidths(contentWidth)
	m.diffSidebarW = leftW
	m.diffViewWidth = rightW
	m.diffViewHeight = contentHeight
	m.rebuildDiffTree()
	m.clampDiffScroll()
}

func (m *model) ensureSelectionVisible() {
	visible := max(0, m.height-listHeaderLines-listFooterLines)
	if visible <= 0 || len(m.tasks) == 0 {
		m.offset = 0
		return
	}
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+visible {
		m.offset = m.selected - visible + 1
	}
	maxOffset := max(0, len(m.tasks)-visible)
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m *model) updateTabContent() {
	m.updateOverviewContent()
	m.updateConflictsContent()
}

func (m *model) updateOverviewContent() {
	if m.detail.Task == nil {
		m.vpOverview.SetContent(styleDim.Render("No task data."))
		m.overviewLayout = overviewSelectionLayout{}
		return
	}

	tk := m.detail.Task
	st := m.detail.State

	// Side-by-side layout: Description (left) | Progress + Metadata (right)
	contentWidth := m.vpOverview.Width
	leftW := contentWidth * 2 / 3
	rightW := contentWidth - leftW - 2 // 2 for gap
	if leftW < 30 {
		leftW = 30
	}
	if rightW < 20 {
		rightW = 20
	}

	// LEFT: Task description
	var leftLines []string
	if strings.TrimSpace(tk.Description) != "" {
		md := renderMarkdown(leftW, tk.Description)
		leftLines = append(leftLines, strings.Split(md, "\n")...)
	} else {
		leftLines = append(leftLines, styleDim.Render("(no description)"))
	}

	// RIGHT: Progress + Details + Workflow in a box
	// lipgloss Width = content+padding, border adds 2 more, so innerW = rightW - 2
	innerW := rightW - 2
	if innerW < 10 {
		innerW = 10
	}

	var sections [][]string

	// Progress section
	if len(m.detail.ProgressSteps) > 0 {
		var progressLines []string
		done, total := task.CountProgressSteps(m.detail.ProgressSteps)
		progressLines = append(progressLines, styleBold.Render("Progress")+styleDim.Render(fmt.Sprintf(" %d/%d", done, total)))
		progressLines = append(progressLines, "")

		for _, s := range m.detail.ProgressSteps {
			var checkbox, text string
			if s.Done {
				checkbox = styleSuccess.Render("■")
				text = styleDim.Render(s.Step)
			} else {
				checkbox = styleDim.Render("□")
				text = s.Step
			}
			prefix := checkbox + " "
			prefixWidth := 2
			stepLines := wrapWithIndent(text, innerW, prefixWidth)
			progressLines = append(progressLines, prefix+stepLines[0])
			progressLines = append(progressLines, stepLines[1:]...)
		}
		sections = append(sections, progressLines)
	}

	// Details section
	var detailsLines []string
	detailsLines = append(detailsLines, styleBold.Render("Details"))
	detailsLines = append(detailsLines, "")

	const labelWidth = 10

	if tk.BaseBranch != "" {
		detailsLines = append(detailsLines, styleDim.Render(padRight("Base", labelWidth))+tk.BaseBranch)
	}
	if m.detail.Model != "" {
		modelInfo := m.detail.Model
		if strings.TrimSpace(m.detail.Reasoning) != "" {
			modelInfo += styleDim.Render(" (" + strings.TrimSpace(m.detail.Reasoning) + ")")
		}
		detailsLines = append(detailsLines, styleDim.Render(padRight("Model", labelWidth))+modelInfo)
	}
	if m.detail.LinesAdded > 0 || m.detail.LinesRemoved > 0 {
		changesInfo := styleSuccess.Render(fmt.Sprintf("+%d", m.detail.LinesAdded)) +
			" " + styleError.Render(fmt.Sprintf("-%d", m.detail.LinesRemoved))
		detailsLines = append(detailsLines, styleDim.Render(padRight("Changes", labelWidth))+changesInfo)
		if m.detail.CommitsBehind > 0 {
			behindMsg := styleStatusReplied.Render(fmt.Sprintf("%d commits behind", m.detail.CommitsBehind))
			detailsLines = append(detailsLines, strings.Repeat(" ", labelWidth)+behindMsg)
		}
	}
	if m.detail.ProgressMeta != nil {
		activityInfo := formatTimeAgo(m.detail.ProgressMeta.LastActive)
		detailsLines = append(detailsLines, styleDim.Render(padRight("Activity", labelWidth))+activityInfo)
	}
	sections = append(sections, detailsLines)

	// Workflow section
	if m.detail.Workflow != nil && strings.TrimSpace(m.detail.Stage) != "" {
		var workflowLines []string
		workflowLines = append(workflowLines, styleBold.Render("Workflow"))
		workflowLines = append(workflowLines, "")
		stageLines := formatStageProgression(m.detail.Workflow.StageNames(), m.detail.Stage, innerW)
		workflowLines = append(workflowLines, strings.Split(stageLines, "\n")...)
		sections = append(sections, workflowLines)
	}

	// Error section (outside box, after it)
	var errorLines []string
	if m.detail.WorkerStatus == task.WorkerStatusError && st != nil && strings.TrimSpace(st.LastError) != "" {
		errorLines = append(errorLines, "")
		errorLines = append(errorLines, styleStatusError.Render("Error"))
		for _, line := range strings.Split(st.LastError, "\n") {
			errorLines = append(errorLines, styleDim.Render(line))
		}
	}

	// Join sections with blank lines
	borderColor := lipgloss.AdaptiveColor{Light: "250", Dark: "238"}
	var boxContent []string
	for i, section := range sections {
		if i > 0 {
			boxContent = append(boxContent, "", "") // two blank lines between sections
		}
		boxContent = append(boxContent, section...)
	}

	// Apply box border - width fills rightW (innerW + 2 padding + 2 border = rightW)
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(innerW)

	var rightLines []string
	boxStr := boxStyle.Render(strings.Join(boxContent, "\n"))
	rightLines = append(rightLines, strings.Split(boxStr, "\n")...)

	// Add error section after box
	rightLines = append(rightLines, errorLines...)

	// Pad columns to viewport height
	targetHeight := m.vpOverview.Height
	if targetHeight < 1 {
		targetHeight = 1
	}
	for len(leftLines) < targetHeight {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < targetHeight {
		rightLines = append(rightLines, "")
	}

	// Join sides with lipgloss
	leftCol := lipgloss.NewStyle().Width(leftW).Render(strings.Join(leftLines, "\n"))
	rightCol := lipgloss.NewStyle().Width(rightW).Render(strings.Join(rightLines, "\n"))

	combined := lipgloss.JoinHorizontal(lipgloss.Top, leftCol, "  ", rightCol)
	m.vpOverview.SetContent(combined)

	m.overviewLayout = overviewSelectionLayout{
		leftW:  leftW,
		rightW: rightW,
		gapW:   2,
	}
}

func (m *model) updateConflictsContent() {
	if len(m.detail.ConflictFiles) == 0 {
		m.vpConflicts.SetContent(styleDim.Render("No conflicts."))
		return
	}

	var lines []string
	lines = append(lines, styleStatusError.Render(fmt.Sprintf("⚠ %d file(s) with conflicts", len(m.detail.ConflictFiles))))
	lines = append(lines, "")
	lines = append(lines, styleDim.Render("Resolve conflicts in the workspace, then retry merge:"))
	lines = append(lines, "")

	for _, f := range m.detail.ConflictFiles {
		lines = append(lines, "  "+styleError.Render("•")+" "+f)
	}

	if m.detail.State != nil && m.detail.State.Workspace != "" {
		lines = append(lines, "")
		lines = append(lines, styleDim.Render("Workspace: ")+m.detail.State.Workspace)
	}

	m.vpConflicts.SetContent(strings.Join(lines, "\n"))
}

func (m *model) updateConversationContent() {
	if m.conversationErr != nil {
		m.vpConversation.SetContent(styleStatusError.Render(m.conversationErr.Error()))
		return
	}

	width := max(20, m.width-2)
	var lines []string
	if strings.TrimSpace(m.conversationHeader.Harness) != "" {
		lines = append(lines, kv("Harness", m.conversationHeader.Harness))
	}
	if strings.TrimSpace(m.conversationHeader.Session) != "" {
		lines = append(lines, kv("Session", m.conversationHeader.Session))
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}

	for _, item := range m.conversationItems {
		if item.IsEvent {
			// Skip redundant events
			if item.Event.Type == "worker.finished" || item.Event.Type == "worker.started" {
				continue
			}
			// Render lifecycle event with filled circle
			ev := item.Event
			eventText := "● " + ev.Text
			if !ev.Time.IsZero() {
				eventText += "  " + formatTimeAgo(ev.Time)
			}
			lines = append(lines, styleDim.Render(eventText))
			lines = append(lines, "") // blank line after event
			continue
		}

		// Render message
		msg := item.Message
		var hdr, border string
		switch msg.Role {
		case task.ConversationRoleLead:
			border = styleStatusDraft.Render("┃")
			hdr = styleStatusDraft.Bold(true).Render("Lead")
		case task.ConversationRoleWorker:
			border = styleStatusReplied.Render("┃")
			hdr = styleStatusReplied.Bold(true).Render("Worker")
		default:
			border = styleDim.Render("┃")
			hdr = styleBold.Render(string(msg.Role))
		}
		if !msg.Time.IsZero() {
			hdr += "  " + styleDim.Render(formatTimeAgo(msg.Time))
		}
		lines = append(lines, border+" "+hdr)
		body := renderMarkdown(width-2, msg.Body) // Account for border + space
		if body == "" {
			body = styleDim.Render("(empty)")
		}

		for _, line := range strings.Split(body, "\n") {
			lines = append(lines, border+" "+line)
		}

		lines = append(lines, "") // blank line between messages (no border)
	}

	follow := m.conversationFollow
	m.vpConversation.SetContent(strings.TrimRight(strings.Join(lines, "\n"), "\n"))
	if follow {
		m.vpConversation.GotoBottom()
	}

}

func (m *model) onTabActivated() tea.Cmd {
	if m.selectedTaskName == "" {
		return nil
	}
	if m.detailTaskName != m.selectedTaskName {
		return fetchDetailCmd(m.selectedTaskName)
	}
	switch m.tab {
	case tabConversation:
		return fetchConversationCmd(m.selectedTaskName)
	case tabDiff:
		// If we already have files for this task, just do UI setup
		if m.diffTaskName == m.selectedTaskName && len(m.diffFiles) > 0 {
			m.rebuildDiffFiltered()
			m.rebuildDiffTree()
			return m.selectDiffPath(m.diffCurrentPath)
		}
		return fetchDiffFilesCmd(m.selectedTaskName, m.detail)
	default:
		return nil
	}
}

func (m model) updateActiveViewport(msg tea.Msg) (model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.tab {
	case tabOverview:
		m.vpOverview, cmd = m.vpOverview.Update(msg)
	case tabConversation:
		m.vpConversation, cmd = m.vpConversation.Update(msg)
		m.conversationFollow = m.vpConversation.AtBottom()
	case tabDiff:
		if mm, ok := msg.(tea.MouseMsg); ok && mm.Action == tea.MouseActionPress {
			switch mm.Button { //nolint:exhaustive
			case tea.MouseButtonWheelUp:
				m.scrollDiff(-changesDefaultScrollLines)
			case tea.MouseButtonWheelDown:
				m.scrollDiff(changesDefaultScrollLines)
			}
		}
	case tabConflicts:
		m.vpConflicts, cmd = m.vpConflicts.Update(msg)
	}
	return m, cmd
}

func (m *model) scrollActiveViewport(delta int) tea.Cmd {
	switch m.tab {
	case tabOverview:
		scrollViewport(&m.vpOverview, delta)
	case tabConversation:
		scrollViewport(&m.vpConversation, delta)
		m.conversationFollow = m.vpConversation.AtBottom()
	case tabDiff:
		m.scrollDiff(delta)
	case tabConflicts:
		scrollViewport(&m.vpConflicts, delta)
	}
	return nil
}

func (m *model) pageActiveViewport(delta int) tea.Cmd {
	switch m.tab {
	case tabOverview:
		pageViewport(&m.vpOverview, delta)
	case tabConversation:
		pageViewport(&m.vpConversation, delta)
		m.conversationFollow = m.vpConversation.AtBottom()
	case tabDiff:
		m.pageDiff(delta)
	case tabConflicts:
		pageViewport(&m.vpConflicts, delta)
	}
	return nil
}

func scrollViewport(vp *viewport.Model, delta int) {
	if delta > 0 {
		_ = vp.ScrollDown(delta)
		return
	}
	if delta < 0 {
		_ = vp.ScrollUp(-delta)
	}
}

func pageViewport(vp *viewport.Model, delta int) {
	if delta > 0 {
		_ = vp.PageDown()
		return
	}
	if delta < 0 {
		_ = vp.PageUp()
	}
}

func diffPaneWidths(total int) (left, right int) {
	if total <= 0 {
		return 0, 0
	}
	left = total / 5 // 20% for file tree
	left = max(20, min(40, left))
	right = max(0, total-left-1)
	return left, right
}

func kv(k, v string) string {
	k = strings.TrimSpace(k)
	v = strings.TrimSpace(v)
	if k == "" {
		return v
	}
	if v == "" {
		v = styleDim.Render("—")
	}
	return styleDim.Render(k) + "  " + v
}

var styleCurrentStage = lipgloss.NewStyle().
	Foreground(lipgloss.Color("63")).
	Bold(true)

var styleOtherStage = styleDim

func formatStageProgression(stages []string, current string, width int) string {
	if len(stages) == 0 {
		return ""
	}

	arrow := styleDim.Render(" → ")
	arrowW := ansi.StringWidth(arrow)

	// Build styled stages with arrows
	var lines []string
	var line string
	lineWidth := 0

	for i, s := range stages {
		var styled string
		if s == current && current != "" {
			styled = styleCurrentStage.Render(s)
		} else {
			styled = styleOtherStage.Render(s)
		}
		w := ansi.StringWidth(styled)

		// Add arrow before this stage (except first)
		needsArrow := i > 0
		extraW := 0
		if needsArrow {
			extraW = arrowW
		}

		if lineWidth > 0 && lineWidth+extraW+w > width {
			// Wrap to new line
			lines = append(lines, line)
			line = styled
			lineWidth = w
		} else {
			if needsArrow {
				line += arrow
				lineWidth += arrowW
			}
			line += styled
			lineWidth += w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// wrapWithIndent wraps text to width, indenting continuation lines by indentWidth spaces.
// Returns at least one line (possibly empty).
func wrapWithIndent(text string, width, indentWidth int) []string {
	if width <= indentWidth {
		return []string{text}
	}

	// Available width for first line (full width minus prefix already added by caller)
	firstLineWidth := width - indentWidth
	// Available width for continuation lines (after indent)
	contLineWidth := width - indentWidth

	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	var currentLine strings.Builder
	currentWidth := 0
	indent := strings.Repeat(" ", indentWidth)

	for _, word := range words {
		wordLen := ansi.StringWidth(word) // Use visual width, not byte length
		maxWidth := firstLineWidth
		if len(lines) > 0 {
			maxWidth = contLineWidth
		}

		if currentWidth == 0 {
			// First word on line
			currentLine.WriteString(word)
			currentWidth = wordLen
		} else if currentWidth+1+wordLen <= maxWidth {
			// Word fits on current line
			currentLine.WriteString(" ")
			currentLine.WriteString(word)
			currentWidth += 1 + wordLen
		} else {
			// Word doesn't fit, start new line
			lines = append(lines, currentLine.String())
			currentLine.Reset()
			currentLine.WriteString(indent)
			currentLine.WriteString(word)
			currentWidth = wordLen
		}
	}

	if currentLine.Len() > 0 {
		lines = append(lines, currentLine.String())
	}

	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}
