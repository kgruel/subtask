package tui

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	osc52 "github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
)

const (
	selectionDragThreshold = 1 // cells (min movement to convert click → selection)
)

type selectionBounds struct {
	minX int
	maxX int // exclusive
	minY int
	maxY int // exclusive
}

func (b selectionBounds) empty() bool { return b.maxX <= b.minX || b.maxY <= b.minY }

func (b selectionBounds) clampPoint(x, y int) (int, int) {
	return clampInt(x, b.minX, b.maxX-1), clampInt(y, b.minY, b.maxY-1)
}

type mouseSelection struct {
	pressed bool

	pressX       int
	pressY       int
	clickPending bool

	selecting bool
	selected  bool

	startX int
	startY int

	endX int // exclusive
	endY int // inclusive

	bounds selectionBounds
	ignore map[string]struct{}
}

func (s mouseSelection) hasSelection() bool {
	if !s.selected {
		return false
	}
	return s.endX != s.startX || s.endY != s.startY
}

func (s mouseSelection) selectionArea() uv.Rectangle {
	sel := uv.Rectangle{
		Min: uv.Pos(s.startX, s.startY),
		Max: uv.Pos(s.endX, s.endY),
	}
	sel = sel.Canon()
	sel.Max.Y++ // make max Y exclusive
	return sel
}

func (m *model) selectionClear() {
	m.mouseSel = mouseSelection{}
}

func (m *model) selectionBoundsForPress(msg tea.MouseMsg) selectionBounds {
	// Default: allow selecting anywhere on screen.
	out := selectionBounds{minX: 0, maxX: max(0, m.width), minY: 0, maxY: max(0, m.height)}
	if out.empty() {
		return out
	}

	// Pane-aware bounds prevent selecting “through” split layouts (sidebar bleed).
	switch {
	case m.mode == viewDetail && m.tab == tabDiff && zone.Get(zoneDiffFilesPane()).InBounds(msg):
		zi := zone.Get(zoneDiffFilesPane())
		if zi != nil && !zi.IsZero() {
			out.minX, out.maxX = zi.StartX, zi.EndX+1
		}
	case m.mode == viewDetail && m.tab == tabDiff && zone.Get(zoneDiffCodePane()).InBounds(msg):
		zi := zone.Get(zoneDiffCodePane())
		if zi != nil && !zi.IsZero() {
			out.minX, out.maxX = zi.StartX, zi.EndX+1
		}
	case m.mode == viewDetail && m.tab == tabOverview && zone.Get(zoneOverviewPane()).InBounds(msg):
		zi := zone.Get(zoneOverviewPane())
		layout := m.overviewLayout
		if zi != nil && !zi.IsZero() && layout.leftW > 0 && layout.rightW > 0 && layout.gapW >= 0 {
			localX := msg.X - zi.StartX
			if localX >= 0 && localX < layout.leftW {
				out.minX, out.maxX = zi.StartX, zi.StartX+layout.leftW
			} else {
				rightStart := layout.leftW + layout.gapW
				rightEnd := rightStart + layout.rightW
				if localX >= rightStart && localX < rightEnd {
					out.minX, out.maxX = zi.StartX+rightStart, zi.StartX+rightEnd
				}
			}
		}
	case m.mode == viewDetail && m.tab == tabConversation && zone.Get(zoneConversationPane()).InBounds(msg):
		zi := zone.Get(zoneConversationPane())
		if zi != nil && !zi.IsZero() {
			// Skip the left message border ("┃ ").
			out.minX, out.maxX = min(zi.StartX+2, zi.EndX+1), zi.EndX+1
		}
	}

	out.minX = clampInt(out.minX, 0, m.width)
	out.maxX = clampInt(out.maxX, 0, m.width)
	if out.maxX < out.minX {
		out.maxX = out.minX
	}
	return out
}

func (m *model) selectionPress(msg tea.MouseMsg) {
	m.mouseSel = mouseSelection{
		pressed:      true,
		pressX:       msg.X,
		pressY:       msg.Y,
		clickPending: true,
		bounds:       m.selectionBoundsForPress(msg),
	}
}

func (m *model) selectionMoveTo(x, y int) {
	if !m.mouseSel.selected || m.mouseSel.bounds.empty() {
		return
	}

	cx, cy := m.mouseSel.bounds.clampPoint(x, y)

	endX := cx
	if cx != m.mouseSel.startX || cy != m.mouseSel.startY {
		if cy > m.mouseSel.startY || (cy == m.mouseSel.startY && cx >= m.mouseSel.startX) {
			endX = cx + 1
		}
	}

	m.mouseSel.endX = clampInt(endX, m.mouseSel.bounds.minX, m.mouseSel.bounds.maxX)
	m.mouseSel.endY = cy
}

func (m *model) selectionMotion(msg tea.MouseMsg) {
	if !m.mouseSel.pressed || m.mouseSel.bounds.empty() {
		return
	}

	if !m.mouseSel.selecting {
		if abs(msg.X-m.mouseSel.pressX) < selectionDragThreshold && abs(msg.Y-m.mouseSel.pressY) < selectionDragThreshold {
			return
		}
		sx, sy := m.mouseSel.bounds.clampPoint(m.mouseSel.pressX, m.mouseSel.pressY)
		m.mouseSel.selected = true
		m.mouseSel.selecting = true
		m.mouseSel.clickPending = false
		m.mouseSel.startX, m.mouseSel.startY = sx, sy
		m.mouseSel.endX, m.mouseSel.endY = sx, sy
	}

	m.selectionMoveTo(msg.X, msg.Y)
}

func (m *model) selectionRelease(msg tea.MouseMsg) tea.Cmd {
	if !m.mouseSel.pressed {
		return nil
	}
	m.mouseSel.pressed = false

	if m.mouseSel.selecting {
		m.selectionMoveTo(msg.X, msg.Y)
		m.mouseSel.selecting = false
		if m.mouseSel.hasSelection() {
			return m.selectionCopy()
		}
		m.selectionClear()
		return nil
	}

	if m.mouseSel.clickPending {
		m.selectionClear()
		if m.mode == viewList {
			return m.handleListClick(msg)
		}
		if m.mode == viewDetail {
			return m.handleDetailClick(msg)
		}
	}

	m.selectionClear()
	return nil
}

func (m *model) selectionCopy() tea.Cmd {
	if !m.mouseSel.hasSelection() {
		return nil
	}

	text := m.selectionGetText()
	if strings.TrimSpace(text) == "" {
		return nil
	}

	// Clear selection first, then show toast.
	m.selectionClear()
	m.toast = toastState{
		kind:     toastSuccess,
		text:     "Copied",
		until:    nowFunc().Add(1500 * time.Millisecond),
		floating: true,
	}
	return copyToClipboardCmd(text)
}

func (m model) selectionGetText() string {
	if !m.mouseSel.hasSelection() || m.width <= 0 || m.height <= 0 {
		return ""
	}
	view := m.viewWithoutZoneScan()
	return selectionView(view, m.width, m.height, m.mouseSel.selectionArea(), m.mouseSel.bounds.minX, m.mouseSel.bounds.maxX, m.mouseSel.ignore, true)
}

func (m model) applySelection(view string) string {
	if !m.mouseSel.hasSelection() || m.width <= 0 || m.height <= 0 {
		return view
	}
	return selectionView(view, m.width, m.height, m.mouseSel.selectionArea(), m.mouseSel.bounds.minX, m.mouseSel.bounds.maxX, m.mouseSel.ignore, false)
}

var clipboardWrite = func(text string) error {
	_ = writeOSC52Clipboard(text)
	return clipboard.WriteAll(text)
}

func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		_ = clipboardWrite(text)
		return nil
	}
}

func writeOSC52Clipboard(text string) error {
	seq := osc52.New(text)
	if os.Getenv("TMUX") != "" {
		seq = seq.Tmux()
	} else if os.Getenv("STY") != "" {
		seq = seq.Screen()
	}
	_, err := fmt.Fprint(os.Stderr, seq.String())
	return err
}

var zoneMarkerRe = regexp.MustCompile("\x1b\\[[0-9]+z")

func stripZoneMarkers(s string) string {
	return zoneMarkerRe.ReplaceAllString(s, "")
}

func selectionView(view string, width int, height int, selArea uv.Rectangle, boundsMinX int, boundsMaxX int, ignore map[string]struct{}, textOnly bool) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	view = stripZoneMarkers(view)

	boundsMinX = clampInt(boundsMinX, 0, width)
	boundsMaxX = clampInt(boundsMaxX, 0, width)
	if boundsMaxX < boundsMinX {
		boundsMaxX = boundsMinX
	}

	area := uv.Rect(0, 0, width, height)
	scr := uv.NewScreenBuffer(area.Dx(), area.Dy())
	uv.NewStyledString(view).Draw(scr, area)

	if selArea.Empty() {
		if textOnly {
			return ""
		}
		return scr.Render()
	}

	selArea = selArea.Canon()
	selArea.Max.Y = min(selArea.Max.Y, height)
	selArea.Min.Y = max(selArea.Min.Y, 0)
	selArea.Min.X = max(selArea.Min.X, 0)
	selArea.Max.X = min(selArea.Max.X, width)
	if selArea.Empty() {
		if textOnly {
			return ""
		}
		return scr.Render()
	}

	isIgnored := func(cell *uv.Cell) bool {
		if ignore == nil || cell == nil {
			return false
		}
		_, ok := ignore[cell.Content]
		return ok
	}

	perLineRange := func(y int) (startX, endX int) {
		startX = selArea.Min.X
		endX = selArea.Max.X
		if selArea.Dy() == 1 {
			return startX, endX
		}
		if y > selArea.Min.Y {
			startX = boundsMinX
		}
		if y < selArea.Max.Y-1 {
			endX = boundsMaxX
		}
		return startX, endX
	}

	if textOnly {
		var out strings.Builder
		for y := selArea.Min.Y; y < selArea.Max.Y; y++ {
			startX, endX := perLineRange(y)
			line := extractSelectedLineText(scr, y, startX, endX, isIgnored)
			out.WriteString(line)
			if y < selArea.Max.Y-1 {
				out.WriteByte('\n')
			}
		}
		return strings.TrimRight(out.String(), "\n")
	}

	var selStyle uv.Style
	if lipgloss.HasDarkBackground() {
		selStyle = uv.Style{Bg: ansi.IndexedColor(237), Fg: ansi.IndexedColor(15)}
	} else {
		selStyle = uv.Style{Bg: ansi.IndexedColor(252), Fg: ansi.IndexedColor(0)}
	}

	for y := selArea.Min.Y; y < selArea.Max.Y; y++ {
		startX, endX := perLineRange(y)
		highlightSelectedRange(scr, y, startX, endX, isIgnored, selStyle)
	}

	return scr.Render()
}

func extractSelectedLineText(scr uv.Screen, y int, startX int, endX int, isIgnored func(*uv.Cell) bool) string {
	if startX < 0 {
		startX = 0
	}
	if endX <= startX {
		return ""
	}
	var line strings.Builder
	var pending strings.Builder
	dropLeadingSpace := false
	for x := startX; x < endX; {
		cell := scr.CellAt(x, y)
		if cell == nil || cell.IsZero() {
			x++
			continue
		}
		if isIgnored(cell) {
			if line.Len() == 0 && pending.Len() == 0 {
				dropLeadingSpace = true
			}
			x += max(1, cell.Width)
			continue
		}
		if cell.Width == 1 && cell.Content == " " {
			if dropLeadingSpace {
				dropLeadingSpace = false
				x++
				continue
			}
			pending.WriteString(" ")
			x++
			continue
		}
		dropLeadingSpace = false
		line.WriteString(pending.String())
		pending.Reset()
		line.WriteString(cell.Content)
		x += max(1, cell.Width)
	}
	return line.String()
}

func highlightSelectedRange(scr uv.Screen, y int, startX int, endX int, isIgnored func(*uv.Cell) bool, selStyle uv.Style) {
	if endX <= startX {
		return
	}

	last := -1
	for x := endX - 1; x >= startX; x-- {
		cell := scr.CellAt(x, y)
		if cell == nil || cell.IsZero() || isIgnored(cell) {
			continue
		}
		if cell.Content != "" && cell.Content != " " {
			last = x
			break
		}
		if cell.Style.Bg != nil {
			last = x
			break
		}
	}
	if last < startX {
		return
	}

	for x := startX; x <= last; {
		cell := scr.CellAt(x, y)
		if cell == nil || cell.IsZero() {
			x++
			continue
		}
		w := max(1, cell.Width)
		if isIgnored(cell) {
			x += w
			continue
		}
		c := cell.Clone()
		c.Style.Bg = selStyle.Bg
		c.Style.Fg = selStyle.Fg
		scr.SetCell(x, y, c)
		x += w
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
