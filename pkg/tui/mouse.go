package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

const doubleClickThreshold = 500 * time.Millisecond

func (m *model) handleListClick(msg tea.MouseMsg) tea.Cmd {
	visible := max(0, m.height-listHeaderLines-listFooterLines)
	start := min(max(0, m.offset), len(m.tasks))
	end := min(len(m.tasks), start+visible)

	for i := start; i < end; i++ {
		t := m.tasks[i]
		id := zoneTaskRow(t.Name)
		if !zone.Get(id).InBounds(msg) {
			continue
		}

		m.selected = i
		m.selectedTaskName = t.Name
		m.ensureSelectionVisible()

		if m.isDoubleClick(id) {
			m.mode = viewDetail
			m.tab = tabOverview
			m.resize()
			return fetchDetailCmd(m.selectedTaskName)
		}

		m.recordClick(id)
		return nil
	}

	return nil
}

func (m *model) handleDetailClick(msg tea.MouseMsg) tea.Cmd {
	// Task strip badges.
	for i, t := range m.tasks {
		id := zoneTaskBadge(t.Name)
		if !zone.Get(id).InBounds(msg) {
			continue
		}
		m.selected = i
		m.selectedTaskName = t.Name
		m.detailTaskName = ""
		m.recordClick(id)
		return fetchDetailCmd(m.selectedTaskName)
	}

	// Tabs.
	for i := 0; i < int(tabCount); i++ {
		t := tab(i)
		id := zoneTab(t)
		if !zone.Get(id).InBounds(msg) {
			continue
		}
		m.tab = t
		m.resize()
		m.recordClick(id)
		return m.onTabActivated()
	}

	// Diff file list (left pane).
	if m.tab == tabDiff {
		for _, p := range m.diffFilteredPaths {
			id := zoneDiffFile(p)
			if !zone.Get(id).InBounds(msg) {
				continue
			}
			m.recordClick(id)
			return m.selectDiffPath(p)
		}
	}

	return nil
}

func (m *model) recordClick(zoneID string) {
	m.lastClickZone = zoneID
	m.lastClickAt = nowFunc()
}

func (m *model) isDoubleClick(zoneID string) bool {
	if m.lastClickZone != zoneID {
		return false
	}
	return nowFunc().Sub(m.lastClickAt) <= doubleClickThreshold
}
