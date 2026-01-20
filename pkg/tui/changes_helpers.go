package tui

import (
	"context"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"
	"github.com/zippoxer/subtask/pkg/diffparse"
	"github.com/zippoxer/subtask/pkg/git"
)

const (
	changesDocCacheMax        = 12
	changesTreeHeaderLines    = 2 // summary + rule
	changesRightHeaderLines   = 2 // file header + rule
	changesSearchHeaderExtra  = 1
	changesDefaultScrollLines = 3
)

func (m *model) rebuildDiffFiltered() {
	query := strings.ToLower(strings.TrimSpace(m.diffSearchInput.Value()))
	paths := make([]string, 0, len(m.diffFiles))
	for _, f := range m.diffFiles {
		if query == "" || strings.Contains(strings.ToLower(f.Path), query) {
			paths = append(paths, f.Path)
		}
	}
	sort.Strings(paths)
	m.diffFilteredPaths = paths
}

func (m *model) rebuildDiffTree() {
	leftW, _ := diffPaneWidths(max(0, m.width-4))
	m.diffSidebarW = leftW
	files := m.filteredDiffFiles()
	lines, idx, orderedPaths := buildChangesTreeLines(files, m.diffCurrentPath, leftW)
	m.diffTreeLines = lines
	m.diffTreeIndex = idx
	m.diffTreeLineCount = len(lines)
	// Use tree order for navigation instead of alphabetical order
	m.diffFilteredPaths = orderedPaths
	// Update selectedIdx to match new ordering
	for i, p := range m.diffFilteredPaths {
		if p == m.diffCurrentPath {
			m.diffSelectedIdx = i
			break
		}
	}
	m.ensureDiffTreeSelectionVisible()
}

func (m *model) filteredDiffFiles() []git.DiffFileStat {
	if len(m.diffFilteredPaths) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(m.diffFilteredPaths))
	for _, p := range m.diffFilteredPaths {
		set[p] = struct{}{}
	}
	out := make([]git.DiffFileStat, 0, len(m.diffFilteredPaths))
	for _, f := range m.diffFiles {
		if _, ok := set[f.Path]; ok {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func (m *model) selectDiffPath(path string) tea.Cmd {
	prevPath := m.diffCurrentPath
	prevScroll := m.diffScrollY
	prevLoading := m.diffLoading

	if len(m.diffFilteredPaths) == 0 {
		m.rebuildDiffFiltered()
	}
	if len(m.diffFilteredPaths) == 0 {
		m.diffCurrentPath = ""
		m.diffHasCurrent = false
		m.diffCurrentFile = git.DiffFileStat{}
		m.diffDoc = nil
		m.diffLoading = false
		m.diffSelectedIdx = 0
		m.diffTreeLines = nil
		m.diffTreeIndex = nil
		m.diffTreeLineCount = 0
		m.diffTreeScroll = 0
		if m.diffLoadCancel != nil {
			m.diffLoadCancel()
			m.diffLoadCancel = nil
		}
		m.clearDiffLayout()
		return nil
	}

	selectedPath := m.diffFilteredPaths[0]
	if path != "" {
		for _, p := range m.diffFilteredPaths {
			if p == path {
				selectedPath = path
				break
			}
		}
	}

	idx := 0
	for i := range m.diffFilteredPaths {
		if m.diffFilteredPaths[i] == selectedPath {
			idx = i
			break
		}
	}
	m.diffSelectedIdx = idx

	file, ok := m.findDiffFile(selectedPath)
	if !ok {
		// Shouldn't happen, but keep UI consistent.
		m.diffCurrentPath = ""
		m.diffHasCurrent = false
		m.diffDoc = nil
		m.diffLoading = false
		return nil
	}

	pathChanged := selectedPath != prevPath
	m.diffCurrentPath = selectedPath
	m.diffCurrentFile = file
	m.diffHasCurrent = true
	if pathChanged {
		m.diffScrollY = 0
	} else {
		m.diffScrollY = prevScroll
	}

	m.rebuildDiffTree()

	if file.Binary {
		m.diffDoc = nil
		m.diffLoading = false
		if m.diffLoadCancel != nil {
			m.diffLoadCancel()
			m.diffLoadCancel = nil
		}
		return nil
	}

	if doc, ok := m.diffDocCache[selectedPath]; ok {
		m.diffDoc = doc
		m.diffLoading = false
		if m.diffLoadCancel != nil {
			m.diffLoadCancel()
			m.diffLoadCancel = nil
		}
		m.clampDiffScroll()
		return nil
	}

	if !pathChanged && prevLoading {
		return nil
	}

	return m.startDiffLoad(selectedPath)
}

func (m *model) findDiffFile(path string) (git.DiffFileStat, bool) {
	for _, f := range m.diffFiles {
		if f.Path == path {
			return f, true
		}
	}
	return git.DiffFileStat{}, false
}

func (m *model) ensureDiffTreeSelectionVisible() {
	if m.diffTreeIndex == nil || !m.diffHasCurrent {
		m.diffTreeScroll = 0
		return
	}
	node, ok := m.diffTreeIndex[m.diffCurrentPath]
	if !ok {
		return
	}

	headerLines := changesTreeHeaderLines
	if m.diffSearchActive {
		headerLines += changesSearchHeaderExtra
	}
	visible := max(0, m.diffViewHeight-headerLines)
	if visible <= 0 {
		m.diffTreeScroll = 0
		return
	}

	if node < m.diffTreeScroll {
		m.diffTreeScroll = node
	} else if node >= m.diffTreeScroll+visible {
		m.diffTreeScroll = node - visible + 1
	}

	maxScroll := max(0, m.diffTreeLineCount-visible)
	m.diffTreeScroll = clampInt(m.diffTreeScroll, 0, maxScroll)
}

func (m *model) clearDiffLayout() {
	m.diffLayoutPath = ""
	m.diffLayoutWidth = 0
	m.diffLayoutSideBySide = false
	m.diffLayoutTotalLines = 0
	m.diffLayoutUnifiedWrap = 0
	m.diffLayoutLineNoW = 0
	m.diffLayoutPrefix = nil
}

func (m *model) startDiffLoad(path string) tea.Cmd {
	if m.diffLoadCancel != nil {
		m.diffLoadCancel()
		m.diffLoadCancel = nil
	}
	m.diffLoadID++
	loadID := m.diffLoadID
	ctx, cancel := context.WithCancel(context.Background())
	m.diffLoadCancel = cancel

	m.diffLoading = true
	m.diffDoc = nil
	m.diffErr = nil
	m.diffScrollY = 0

	return fetchDiffDocCmd(m.selectedTaskName, m.diffCtx, path, loadID, ctx)
}

func (m *model) cacheDiffDoc(path string, doc *diffparse.Document) {
	if doc == nil || path == "" {
		return
	}

	if _, ok := m.diffDocCache[path]; ok {
		for i := range m.diffDocOrder {
			if m.diffDocOrder[i] == path {
				m.diffDocOrder = append(m.diffDocOrder[:i], m.diffDocOrder[i+1:]...)
				break
			}
		}
	}

	m.diffDocCache[path] = doc
	m.diffDocOrder = append(m.diffDocOrder, path)

	for len(m.diffDocOrder) > changesDocCacheMax {
		evict := m.diffDocOrder[0]
		m.diffDocOrder = m.diffDocOrder[1:]
		delete(m.diffDocCache, evict)
	}
}

func (m *model) diffContentHeight() int {
	return max(0, m.diffViewHeight-changesRightHeaderLines)
}

func (m *model) ensureDiffLayout(width int) {
	if m.diffDoc == nil {
		m.clearDiffLayout()
		return
	}

	if width <= 0 {
		width = m.diffViewWidth
	}

	if m.diffLayoutPath == m.diffCurrentPath &&
		m.diffLayoutWidth == width &&
		m.diffLayoutSideBySide == m.diffSideBySide &&
		((m.diffSideBySide && m.diffLayoutTotalLines == len(m.diffDoc.SideBySide)) ||
			(!m.diffSideBySide && m.diffLayoutPrefix != nil)) {
		return
	}

	m.diffLayoutPath = m.diffCurrentPath
	m.diffLayoutWidth = width
	m.diffLayoutSideBySide = m.diffSideBySide
	m.diffLayoutPrefix = nil

	if m.diffSideBySide {
		m.diffLayoutTotalLines = len(m.diffDoc.SideBySide)
		m.diffLayoutUnifiedWrap = 0
		m.diffLayoutLineNoW = 0
		return
	}

	maxLine := max(m.diffDoc.UnifiedOld, m.diffDoc.UnifiedNew)
	lineNoW := max(4, digits(maxLine))
	codeW := max(0, width-(lineNoW+1))
	wrapW := codeW - 2
	if wrapW < 1 {
		wrapW = 0
	}

	prefix := make([]int, len(m.diffDoc.Unified)+1)
	for i, r := range m.diffDoc.Unified {
		height := 1
		if r.Kind != diffparse.KindSeparator && wrapW > 0 {
			w := runewidth.StringWidth(normalizeDiffText(r.Text))
			height = max(1, (w+wrapW-1)/wrapW)
		}
		prefix[i+1] = prefix[i] + height
	}

	m.diffLayoutTotalLines = prefix[len(prefix)-1]
	m.diffLayoutUnifiedWrap = wrapW
	m.diffLayoutLineNoW = lineNoW
	m.diffLayoutPrefix = prefix
}

func (m *model) diffLineCount() int {
	if m.diffDoc == nil {
		return 0
	}
	m.ensureDiffLayout(m.diffViewWidth)
	return m.diffLayoutTotalLines
}

func (m *model) clampDiffScroll() {
	maxY := max(0, m.diffLineCount()-m.diffContentHeight())
	m.diffScrollY = clampInt(m.diffScrollY, 0, maxY)
}

func (m *model) scrollDiff(delta int) {
	if m.diffDoc == nil {
		return
	}
	m.diffScrollY += delta
	m.clampDiffScroll()
}

func (m *model) pageDiff(delta int) {
	n := m.diffContentHeight()
	if n <= 0 {
		return
	}
	if delta > 0 {
		m.scrollDiff(n)
		return
	}
	if delta < 0 {
		m.scrollDiff(-n)
	}
}

func clampInt(v, low, high int) int {
	if high < low {
		low, high = high, low
	}
	return min(high, max(low, v))
}
