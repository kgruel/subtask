package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
	"github.com/zippoxer/subtask/pkg/diffparse"
	"github.com/zippoxer/subtask/pkg/git"
	"github.com/zippoxer/subtask/pkg/logging"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/task/store"
)

type viewMode int

const (
	viewList viewMode = iota
	viewDetail
)

type actionKind int

const (
	actionNone actionKind = iota
	actionMerge
	actionClose
	actionAbandon
)

type confirmState struct {
	kind     actionKind
	taskName string
}

func (c confirmState) active() bool { return c.kind != actionNone && c.taskName != "" }

type alertState struct {
	title string
	body  string
}

func (a alertState) active() bool { return a.title != "" || a.body != "" }

type toastKind int

const (
	toastNone toastKind = iota
	toastInfo
	toastSuccess
	toastError
)

type toastState struct {
	kind     toastKind
	text     string
	until    time.Time
	floating bool // render as top-right overlay instead of status line
}

func (t toastState) active() bool { return t.kind != toastNone && t.text != "" }

type listLoadedMsg struct {
	data store.ListResult
	err  error
}

type tickMsg time.Time
type spinnerTickMsg time.Time

// Spinner frames (Braille dots)
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type detailLoadedMsg struct {
	taskName string
	detail   store.TaskView
	err      error
}

type conversationLoadedMsg struct {
	taskName string
	header   task.ConversationHeader
	items    []task.ConversationItem
	err      error
}

type diffMode int

const (
	diffModeWorkspace diffMode = iota
	diffModeRange
)

type diffCtx struct {
	mode   diffMode
	dir    string
	base   string
	branch string // only for range diffs
}

type diffFilesLoadedMsg struct {
	taskName string
	ctx      diffCtx
	files    []git.DiffFileStat
	err      error
}

type diffFileLoadedMsg struct {
	taskName string
	ctx      diffCtx
	path     string
	doc      *diffparse.Document
	loadID   int
	err      error
}

type model struct {
	mode viewMode
	tab  tab

	width  int
	height int

	tasks               []store.TaskListItem
	filteredTasks       []store.TaskListItem // filtered view when searching
	availableWorkspaces int
	selected            int
	offset              int

	selectedTaskName string

	// Search
	searchInput  textinput.Model
	searchActive bool

	listErr error

	showHelp bool
	confirm  confirmState
	alert    alertState
	toast    toastState

	busy actionKind
	// disableTicker disables automatic 2s refresh ticks (tests can manually refresh with 'r').
	disableTicker bool

	// Spinner for working status animation
	spinnerFrame int

	// detail data (refreshed on demand)
	detailTaskName string
	detail         store.TaskView
	detailErr      error

	// viewports (one per tab; diff uses split-pane viewport)
	vpOverview     viewport.Model
	vpConversation viewport.Model
	vpConflicts    viewport.Model

	// conversation tab data
	conversationTaskName string
	conversationHeader   task.ConversationHeader
	conversationItems    []task.ConversationItem
	conversationErr      error
	conversationFollow   bool

	// diff tab data
	diffTaskName    string
	diffCtx         diffCtx
	diffFiles       []git.DiffFileStat
	diffErr         error
	diffCurrentPath string
	diffCurrentFile git.DiffFileStat
	diffHasCurrent  bool

	// changes view state
	diffSideBySide   bool // default false (unified)
	diffSearchInput  textinput.Model
	diffSearchActive bool

	diffFilteredPaths []string
	diffSelectedIdx   int // index into diffFilteredPaths

	diffTreeLines     []string
	diffTreeIndex     map[string]int // path -> y offset in diffTreeLines
	diffTreeLineCount int
	diffTreeScroll    int

	diffDocCache   map[string]*diffparse.Document
	diffDocOrder   []string
	diffDoc        *diffparse.Document
	diffLoading    bool
	diffLoadID     int
	diffLoadCancel context.CancelFunc
	diffScrollY    int
	diffViewWidth  int
	diffViewHeight int
	diffSidebarW   int

	// diff layout cache (unified wrapping + scroll mapping)
	diffLayoutPath        string
	diffLayoutWidth       int
	diffLayoutSideBySide  bool
	diffLayoutTotalLines  int
	diffLayoutUnifiedWrap int
	diffLayoutLineNoW     int
	diffLayoutPrefix      []int

	// mouse: simple double-click detection
	lastClickAt   time.Time
	lastClickZone string

	// mouse selection (browser-like): click+drag selects text and auto-copies on release
	mouseSel mouseSelection

	// selection layout metadata for region-aware selection
	overviewLayout overviewSelectionLayout
}

func newModel() model {
	ti := textinput.New()
	ti.Placeholder = "filter..."
	ti.CharLimit = 100
	ti.Width = 20

	m := model{
		mode:               viewList,
		tab:                tabOverview,
		conversationFollow: true,
		diffDocCache:       make(map[string]*diffparse.Document),
		diffSideBySide:     false,
		searchInput:        ti,
	}
	di := textinput.New()
	di.Placeholder = "filter files..."
	di.CharLimit = 200
	di.Width = 20
	m.diffSearchInput = di
	m.vpOverview = viewport.New(0, 0)
	m.vpConversation = viewport.New(0, 0)
	m.vpConflicts = viewport.New(0, 0)
	return m
}

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{fetchListCmd()}
	if !m.disableTicker {
		cmds = append(cmds, tickCmd(), spinnerTickCmd())
	}
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m.expireToast()

	// Always let viewports process mouse wheel events in detail view.
	if m.mode == viewDetail {
		if _, ok := msg.(tea.MouseMsg); ok {
			var cmd tea.Cmd
			m, cmd = m.updateActiveViewport(msg)
			if cmd != nil {
				return m, cmd
			}
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.selectionClear()
		m.resize()
		return m, nil

	case mergeDoneMsg:
		m.busy = actionNone
		m.diffDocCache = make(map[string]*diffparse.Document)
		m.diffDocOrder = nil
		m.diffDoc = nil
		m.diffLoading = false
		m.diffTreeLines = nil
		m.diffTreeIndex = nil
		m.diffTreeLineCount = 0
		m.diffTreeScroll = 0
		m.diffFilteredPaths = nil
		m.diffSelectedIdx = 0
		m.diffHasCurrent = false
		m.diffCurrentPath = ""
		m.clearDiffLayout()
		if m.diffLoadCancel != nil {
			m.diffLoadCancel()
			m.diffLoadCancel = nil
		}

		if msg.err != nil {
			m.toast = toastState{kind: toastError, text: "Merge failed", until: nowFunc().Add(6 * time.Second)}
			m.alert = alertState{title: "Merge failed", body: msg.err.Error()}
		} else if msg.res.AlreadyClosed {
			if msg.res.AlreadyMerged {
				m.toast = toastState{kind: toastInfo, text: "Task " + msg.taskName + " is already merged.", until: nowFunc().Add(4 * time.Second)}
			} else {
				m.toast = toastState{kind: toastInfo, text: "Task " + msg.taskName + " is already closed.", until: nowFunc().Add(4 * time.Second)}
			}
		} else if msg.baseBranch != "" {
			m.toast = toastState{kind: toastSuccess, text: "Merged " + msg.taskName + " into " + msg.baseBranch + ".", until: nowFunc().Add(4 * time.Second)}
		} else {
			m.toast = toastState{kind: toastSuccess, text: "Merged " + msg.taskName + ".", until: nowFunc().Add(4 * time.Second)}
		}

		return m, m.refreshSelected()

	case closeDoneMsg:
		m.busy = actionNone
		m.diffDocCache = make(map[string]*diffparse.Document)
		m.diffDocOrder = nil
		m.diffDoc = nil
		m.diffLoading = false
		m.diffTreeLines = nil
		m.diffTreeIndex = nil
		m.diffTreeLineCount = 0
		m.diffTreeScroll = 0
		m.diffFilteredPaths = nil
		m.diffSelectedIdx = 0
		m.diffHasCurrent = false
		m.diffCurrentPath = ""
		m.clearDiffLayout()
		if m.diffLoadCancel != nil {
			m.diffLoadCancel()
			m.diffLoadCancel = nil
		}

		action := "Closed"
		if msg.abandon {
			action = "Abandoned"
		}

		if msg.err != nil {
			m.toast = toastState{kind: toastError, text: action + " failed", until: nowFunc().Add(6 * time.Second)}
			m.alert = alertState{title: action + " failed", body: msg.err.Error()}
		} else if msg.res.AlreadyClosed {
			m.toast = toastState{kind: toastInfo, text: "Task " + msg.taskName + " is already closed.", until: nowFunc().Add(4 * time.Second)}
		} else {
			m.toast = toastState{kind: toastSuccess, text: action + " " + msg.taskName + ".", until: nowFunc().Add(4 * time.Second)}
		}

		return m, m.refreshSelected()

	case listLoadedMsg:
		m.listErr = msg.err
		if msg.err != nil {
			logging.Error("tui", "refresh list error: "+msg.err.Error())
			return m, nil
		}
		if logging.DebugEnabled() {
			logging.Debug("tui", fmt.Sprintf("data arrived items=%d (+%s)", len(msg.data.Tasks), sinceStartup().Round(time.Millisecond)))
		}
		m.tasks = msg.data.Tasks
		m.availableWorkspaces = msg.data.AvailableWorkspaces

		// Re-filter if search is active (without resetting selection)
		if m.searchActive && m.searchInput.Value() != "" {
			m.refilterTasks()
		}

		// Clamp selection to visible list, preserving selected task by name
		visible := m.visibleTasks()
		m.selected = clampSelection(visible, m.selected, m.selectedTaskName)
		if m.selected >= 0 && m.selected < len(visible) {
			m.selectedTaskName = visible[m.selected].Name
		} else {
			m.selectedTaskName = ""
		}
		m.ensureSelectionVisible()

		// Keep detail view consistent after refreshes that reorder or remove tasks.
		if m.mode == viewDetail && m.selectedTaskName != "" && m.detailTaskName != m.selectedTaskName {
			return m, fetchDetailCmd(m.selectedTaskName)
		}
		return m, nil

	case detailLoadedMsg:
		if msg.taskName != m.selectedTaskName {
			return m, nil
		}
		m.detailErr = msg.err
		m.detailTaskName = msg.taskName
		if msg.err != nil {
			return m, nil
		}
		m.detail = msg.detail
		m.updateTabContent()

		var cmds []tea.Cmd
		if m.mode == viewDetail && m.tab == tabConversation {
			cmds = append(cmds, fetchConversationCmd(m.selectedTaskName))
		}
		// Always fetch diff files for the tab count
		if m.mode == viewDetail {
			cmds = append(cmds, fetchDiffFilesCmd(m.selectedTaskName, m.detail))
		}
		return m, tea.Batch(cmds...)

	case conversationLoadedMsg:
		if msg.taskName != m.selectedTaskName || m.tab != tabConversation {
			return m, nil
		}
		m.conversationErr = msg.err
		m.conversationTaskName = msg.taskName
		m.conversationHeader = msg.header
		m.conversationItems = msg.items
		m.updateConversationContent()
		return m, nil

	case diffFilesLoadedMsg:
		if msg.taskName != m.selectedTaskName {
			return m, nil
		}
		m.diffErr = msg.err
		m.diffTaskName = msg.taskName
		m.diffFiles = msg.files

		// Only do full UI setup when on the diff tab
		if m.tab != tabDiff {
			m.diffCtx = msg.ctx
			return m, nil
		}

		if msg.ctx != m.diffCtx {
			m.diffDocCache = make(map[string]*diffparse.Document)
			m.diffDocOrder = nil
			m.diffDoc = nil
			m.diffLoading = false
			m.diffTreeLines = nil
			m.diffTreeIndex = nil
			m.diffTreeLineCount = 0
			m.diffTreeScroll = 0
			m.diffFilteredPaths = nil
			m.diffSelectedIdx = 0
			m.diffHasCurrent = false
			m.diffCurrentPath = ""
			m.clearDiffLayout()
			if m.diffLoadCancel != nil {
				m.diffLoadCancel()
				m.diffLoadCancel = nil
			}
		}
		m.diffCtx = msg.ctx

		if len(m.diffFiles) == 0 {
			m.diffCurrentPath = ""
			m.diffHasCurrent = false
			m.diffCurrentFile = git.DiffFileStat{}
			m.diffDoc = nil
			m.diffLoading = false
			m.diffTreeLines = nil
			m.diffTreeIndex = nil
			m.diffTreeLineCount = 0
			m.diffTreeScroll = 0
			m.diffFilteredPaths = nil
			m.clearDiffLayout()
			return m, nil
		}

		m.rebuildDiffFiltered()
		m.rebuildDiffTree()
		return m, m.selectDiffPath(m.diffCurrentPath)

	case diffFileLoadedMsg:
		if msg.taskName != m.selectedTaskName || m.tab != tabDiff {
			return m, nil
		}
		if msg.ctx != m.diffCtx || msg.loadID != m.diffLoadID {
			return m, nil
		}
		m.diffLoading = false
		if msg.err != nil {
			m.diffErr = msg.err
			m.diffDoc = nil
			return m, nil
		}
		m.diffErr = nil
		m.cacheDiffDoc(msg.path, msg.doc)
		if msg.path == m.diffCurrentPath {
			m.diffDoc = msg.doc
			m.diffScrollY = 0
			m.clearDiffLayout()
			m.clampDiffScroll()
		}
		return m, nil

	case tickMsg:
		m.expireToast()

		var cmds []tea.Cmd
		cmds = append(cmds, fetchListCmd(), tickCmd())
		if m.mode == viewDetail && m.selectedTaskName != "" {
			cmds = append(cmds, fetchDetailCmd(m.selectedTaskName))
			if m.tab == tabConversation {
				cmds = append(cmds, fetchConversationCmd(m.selectedTaskName))
			}
			if m.tab == tabDiff {
				cmds = append(cmds, fetchDiffFilesCmd(m.selectedTaskName, m.detail))
			}
		}
		return m, tea.Batch(cmds...)

	case spinnerTickMsg:
		m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
		return m, spinnerTickCmd()

	case tea.KeyMsg:
		if m.alert.active() {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc", "enter", "q":
				m.alert = alertState{}
				return m, nil
			default:
				return m, nil
			}
		}

		if m.confirm.active() {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "n", "esc":
				m.confirm = confirmState{}
				return m, nil
			case "y":
				kind := m.confirm.kind
				taskName := m.confirm.taskName
				m.confirm = confirmState{}
				m.busy = kind

				switch kind {
				case actionMerge:
					m.toast = toastState{kind: toastInfo, text: "Merging " + taskName + "..."}
					return m, mergeTaskCmd(taskName)
				case actionClose:
					m.toast = toastState{kind: toastInfo, text: "Closing " + taskName + "..."}
					return m, closeTaskCmd(taskName, false)
				case actionAbandon:
					m.toast = toastState{kind: toastInfo, text: "Abandoning " + taskName + "..."}
					return m, closeTaskCmd(taskName, true)
				default:
					m.busy = actionNone
					return m, nil
				}
			default:
				return m, nil
			}
		}

		if m.showHelp {
			switch msg.String() {
			case "esc", "q", "?":
				m.showHelp = false
				return m, nil
			default:
				return m, nil
			}
		}

		// Search mode handling
		if m.searchActive && m.mode == viewList {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.searchActive = false
				m.searchInput.Blur()
				m.searchInput.SetValue("")
				m.filteredTasks = nil
				// Restore selection in full list
				m.selected = clampSelection(m.tasks, m.selected, m.selectedTaskName)
				return m, nil
			case "backspace":
				// If empty, exit search mode
				if m.searchInput.Value() == "" {
					m.searchActive = false
					m.searchInput.Blur()
					m.filteredTasks = nil
					m.selected = clampSelection(m.tasks, m.selected, m.selectedTaskName)
					return m, nil
				}
				// Otherwise pass to text input
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				m.updateFilteredTasks()
				return m, cmd
			case "enter":
				// View selected task
				tasks := m.visibleTasks()
				if len(tasks) > 0 {
					m.mode = viewDetail
					m.tab = tabOverview
					m.resize()
					return m, fetchDetailCmd(m.selectedTaskName)
				}
				return m, nil
			case "up":
				tasks := m.visibleTasks()
				if len(tasks) > 0 && m.selected > 0 {
					m.selected--
					m.selectedTaskName = tasks[m.selected].Name
				}
				return m, nil
			case "down":
				tasks := m.visibleTasks()
				if len(tasks) > 0 && m.selected < len(tasks)-1 {
					m.selected++
					m.selectedTaskName = tasks[m.selected].Name
				}
				return m, nil
			default:
				// Pass to text input
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				m.updateFilteredTasks()
				return m, cmd
			}
		}

		// Changes tab file search handling
		if m.diffSearchActive && m.mode == viewDetail && m.tab == tabDiff {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.diffSearchActive = false
				m.diffSearchInput.Blur()
				m.diffSearchInput.SetValue("")
				m.rebuildDiffFiltered()
				m.rebuildDiffTree()
				return m, m.selectDiffPath(m.diffCurrentPath)
			case "backspace":
				if m.diffSearchInput.Value() == "" {
					m.diffSearchActive = false
					m.diffSearchInput.Blur()
					m.rebuildDiffFiltered()
					m.rebuildDiffTree()
					return m, m.selectDiffPath(m.diffCurrentPath)
				}
				var cmd tea.Cmd
				m.diffSearchInput, cmd = m.diffSearchInput.Update(msg)
				_ = cmd
				m.rebuildDiffFiltered()
				m.rebuildDiffTree()
				return m, m.selectDiffPath(m.diffCurrentPath)
			case "enter":
				m.diffSearchActive = false
				m.diffSearchInput.Blur()
				m.rebuildDiffTree()
				return m, m.selectDiffPath(m.diffCurrentPath)
			case "up", "k":
				if m.diffSelectedIdx > 0 && len(m.diffFilteredPaths) > 0 {
					return m, m.selectDiffPath(m.diffFilteredPaths[m.diffSelectedIdx-1])
				}
				return m, nil
			case "down", "j":
				if m.diffSelectedIdx < len(m.diffFilteredPaths)-1 && len(m.diffFilteredPaths) > 0 {
					return m, m.selectDiffPath(m.diffFilteredPaths[m.diffSelectedIdx+1])
				}
				return m, nil
			default:
				var cmd tea.Cmd
				m.diffSearchInput, cmd = m.diffSearchInput.Update(msg)
				_ = cmd
				m.rebuildDiffFiltered()
				m.rebuildDiffTree()
				return m, m.selectDiffPath(m.diffCurrentPath)
			}
		}

		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "/":
			if m.mode == viewList {
				m.searchActive = true
				m.searchInput.Focus()
				return m, nil
			}
			if m.mode == viewDetail && m.tab == tabDiff {
				m.diffSearchActive = true
				m.diffSearchInput.Focus()
				return m, nil
			}
		case "s":
			if m.mode == viewDetail && m.tab == tabDiff {
				m.diffSideBySide = !m.diffSideBySide
				m.clampDiffScroll()
				return m, nil
			}
		case "ctrl+g":
			// Merge (Ctrl+G for "go merge")
			if m.busy != actionNone || m.selectedTaskName == "" {
				return m, nil
			}
			m.confirm = confirmState{kind: actionMerge, taskName: m.selectedTaskName}
			return m, nil
		case "ctrl+d":
			// Close/Delete (Ctrl+D)
			if m.mode == viewList {
				if m.busy != actionNone || m.selectedTaskName == "" {
					return m, nil
				}
				m.confirm = confirmState{kind: actionClose, taskName: m.selectedTaskName}
				return m, nil
			}
			// In detail view, page down
			return m, m.pageActiveViewport(1)
		case "ctrl+x":
			// Abandon (Ctrl+X)
			if m.busy != actionNone || m.selectedTaskName == "" {
				return m, nil
			}
			m.confirm = confirmState{kind: actionAbandon, taskName: m.selectedTaskName}
			return m, nil
		case "?":
			m.showHelp = true
			return m, nil
		case "up", "k":
			if m.mode == viewList {
				if len(m.tasks) == 0 {
					return m, nil
				}
				if m.selected > 0 {
					m.selected--
				}
				m.selectedTaskName = m.tasks[m.selected].Name
				m.ensureSelectionVisible()
				return m, nil
			}
			if m.mode == viewDetail && m.tab == tabDiff {
				if m.diffSelectedIdx > 0 && len(m.diffFilteredPaths) > 0 {
					return m, m.selectDiffPath(m.diffFilteredPaths[m.diffSelectedIdx-1])
				}
				return m, nil
			}
			return m, m.scrollActiveViewport(-1)
		case "down", "j":
			if m.mode == viewList {
				if len(m.tasks) == 0 {
					return m, nil
				}
				if m.selected < len(m.tasks)-1 {
					m.selected++
				}
				m.selectedTaskName = m.tasks[m.selected].Name
				m.ensureSelectionVisible()
				return m, nil
			}
			if m.mode == viewDetail && m.tab == tabDiff {
				if m.diffSelectedIdx < len(m.diffFilteredPaths)-1 && len(m.diffFilteredPaths) > 0 {
					return m, m.selectDiffPath(m.diffFilteredPaths[m.diffSelectedIdx+1])
				}
				return m, nil
			}
			return m, m.scrollActiveViewport(1)
		case "g":
			if m.mode == viewList && len(m.tasks) > 0 {
				m.selected = 0
				m.selectedTaskName = m.tasks[0].Name
				m.ensureSelectionVisible()
			}
			return m, nil
		case "G":
			if m.mode == viewList && len(m.tasks) > 0 {
				m.selected = len(m.tasks) - 1
				m.selectedTaskName = m.tasks[m.selected].Name
				m.ensureSelectionVisible()
			}
			return m, nil
		case "enter":
			if m.mode == viewList && len(m.tasks) > 0 {
				m.mode = viewDetail
				m.tab = tabOverview
				m.resize()
				return m, fetchDetailCmd(m.selectedTaskName)
			}
			if m.mode == viewList {
				return m, nil
			}
		case "esc":
			if m.mode == viewDetail {
				m.mode = viewList
				m.detailTaskName = "" // Clear so re-entering triggers fresh fetch
				return m, nil
			}
		case "left", "h":
			if m.mode == viewDetail && len(m.tasks) > 0 && m.selected > 0 {
				m.selected--
				m.selectedTaskName = m.tasks[m.selected].Name
				m.detailTaskName = ""
				return m, fetchDetailCmd(m.selectedTaskName)
			}
		case "right", "l":
			if m.mode == viewDetail && len(m.tasks) > 0 && m.selected < len(m.tasks)-1 {
				m.selected++
				m.selectedTaskName = m.tasks[m.selected].Name
				m.detailTaskName = ""
				return m, fetchDetailCmd(m.selectedTaskName)
			}
		case "tab":
			if m.mode == viewDetail {
				m.tab = (m.tab + 1) % tabCount
				m.resize()
				return m, m.onTabActivated()
			}
		case "shift+tab":
			if m.mode == viewDetail {
				m.tab = (m.tab - 1 + tabCount) % tabCount
				m.resize()
				return m, m.onTabActivated()
			}
		case "1", "2", "3", "4", "5":
			if m.mode == viewDetail {
				idx := int(msg.String()[0] - '1')
				if idx >= 0 && idx < int(tabCount) {
					m.tab = tab(idx)
					m.resize()
					return m, m.onTabActivated()
				}
			}
		case "pgup":
			if m.mode == viewDetail {
				return m, m.pageActiveViewport(-1)
			}
		case "pgdown":
			if m.mode == viewDetail {
				return m, m.pageActiveViewport(1)
			}
		}

	case tea.MouseMsg:
		// Selection is disabled when overlays are active to avoid confusing interactions.
		overlaysActive := m.showHelp || m.confirm.active() || m.alert.active()
		if overlaysActive {
			m.selectionClear()
			return m, nil
		}

		switch msg.Action {
		case tea.MouseActionPress:
			if msg.Button == tea.MouseButtonLeft {
				m.selectionPress(msg)
			}

		case tea.MouseActionMotion:
			if msg.Button == tea.MouseButtonLeft {
				m.selectionMotion(msg)
			}

		case tea.MouseActionRelease:
			if msg.Button == tea.MouseButtonLeft {
				return m, m.selectionRelease(msg)
			}
		}
	}

	return m, nil
}

func (m model) viewWithoutZoneScan() string {
	var out string
	switch m.mode {
	case viewDetail:
		out = renderDetailView(m)
	default:
		out = renderListView(m)
	}
	if m.toast.floating {
		out = overlayFloatingToast(out, m.toast, m.width, m.height)
	}
	if m.showHelp && !m.confirm.active() && !m.alert.active() {
		out = renderHelpOverlay(m, out)
	}
	if m.confirm.active() {
		out = renderConfirmOverlay(m, out)
	}
	if m.alert.active() {
		out = renderAlertOverlay(m, out)
	}
	return out
}

func (m model) View() string {
	logFirstRenderOnce()
	out := m.viewWithoutZoneScan()
	out = zone.Scan(out)
	return m.applySelection(out)
}

func fetchListCmd() tea.Cmd {
	return func() tea.Msg {
		done := logging.DebugTimer("refresh", "start")
		st := store.New()
		data, err := st.List(context.Background(), store.ListOptions{All: true})
		if err != nil {
			logging.Error("refresh", "store.List error: "+err.Error())
		}
		if logging.DebugEnabled() {
			done(fmt.Sprintf("done items=%d", len(data.Tasks)))
		} else {
			done("")
		}
		return listLoadedMsg{data: data, err: err}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func spinnerTickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return spinnerTickMsg(t) })
}

func (m *model) expireToast() {
	if m.toast.until.IsZero() {
		return
	}
	if nowFunc().After(m.toast.until) {
		m.toast = toastState{}
	}
}

func (m model) refreshSelected() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, fetchListCmd())
	if m.mode == viewDetail && m.selectedTaskName != "" {
		cmds = append(cmds, fetchDetailCmd(m.selectedTaskName))
		if m.tab == tabConversation {
			cmds = append(cmds, fetchConversationCmd(m.selectedTaskName))
		}
		if m.tab == tabDiff {
			cmds = append(cmds, fetchDiffFilesCmd(m.selectedTaskName, m.detail))
		}
	}
	return tea.Batch(cmds...)
}

func clampSelection(tasks []store.TaskListItem, idx int, preferredName string) int {
	if len(tasks) == 0 {
		return 0
	}
	if preferredName != "" {
		for i, t := range tasks {
			if t.Name == preferredName {
				return i
			}
		}
	}
	if idx < 0 {
		return 0
	}
	if idx >= len(tasks) {
		return len(tasks) - 1
	}
	return idx
}

// visibleTasks returns the filtered tasks if search is active, otherwise all tasks.
func (m model) visibleTasks() []store.TaskListItem {
	if m.searchActive && m.searchInput.Value() != "" {
		return m.filteredTasks
	}
	return m.tasks
}

// updateFilteredTasks filters tasks based on the search input and resets selection to first match.
func (m *model) updateFilteredTasks() {
	m.refilterTasks()

	// Reset selection to first match
	m.selected = 0
	m.offset = 0
	if len(m.filteredTasks) > 0 {
		m.selectedTaskName = m.filteredTasks[0].Name
	}
}

// refilterTasks rebuilds filteredTasks without changing selection.
func (m *model) refilterTasks() {
	query := strings.ToLower(m.searchInput.Value())
	if query == "" {
		m.filteredTasks = nil
		return
	}

	m.filteredTasks = nil
	for _, t := range m.tasks {
		if strings.Contains(strings.ToLower(t.Name), query) ||
			strings.Contains(strings.ToLower(t.Title), query) {
			m.filteredTasks = append(m.filteredTasks, t)
		}
	}
}

func fetchDetailCmd(taskName string) tea.Cmd {
	return func() tea.Msg {
		st := store.New()
		d, err := st.Get(context.Background(), taskName, store.GetOptions{})
		return detailLoadedMsg{taskName: taskName, detail: d, err: err}
	}
}

func fetchConversationCmd(taskName string) tea.Cmd {
	return func() tea.Msg {
		events, err := history.Read(taskName, history.ReadOptions{})
		if err != nil {
			return conversationLoadedMsg{taskName: taskName, err: err}
		}

		var h task.ConversationHeader
		var items []task.ConversationItem
		for _, ev := range events {
			switch ev.Type {
			case "worker.session":
				var d struct {
					Harness   string `json:"harness"`
					SessionID string `json:"session_id"`
				}
				if json.Unmarshal(ev.Data, &d) == nil {
					if strings.TrimSpace(d.Harness) != "" {
						h.Harness = strings.TrimSpace(d.Harness)
					}
					if strings.TrimSpace(d.SessionID) != "" {
						h.Session = strings.TrimSpace(d.SessionID)
					}
				}
			case "message":
				role := strings.TrimSpace(ev.Role)
				var r task.ConversationRole
				switch role {
				case "lead":
					r = task.ConversationRoleLead
				case "worker":
					r = task.ConversationRoleWorker
				default:
					r = task.ConversationRole(role)
				}
				items = append(items, task.ConversationItem{
					Message: task.ConversationMessage{Role: r, Body: ev.Content, Time: ev.TS},
				})
			case "task.opened":
				var d struct {
					Reason     string `json:"reason"`
					BaseBranch string `json:"base_branch"`
				}
				_ = json.Unmarshal(ev.Data, &d)
				text := "Task opened"
				if d.Reason == "revive" {
					text = "Task revived"
				}
				if d.BaseBranch != "" {
					text += " (based on " + d.BaseBranch + ")"
				}
				items = append(items, task.ConversationItem{
					IsEvent: true,
					Event:   task.ConversationEvent{Type: ev.Type, Text: text, Time: ev.TS},
				})
			case "task.merged":
				var d struct {
					Into string `json:"into"`
				}
				_ = json.Unmarshal(ev.Data, &d)
				text := "Task merged"
				if d.Into != "" {
					text += " into " + d.Into
				}
				items = append(items, task.ConversationItem{
					IsEvent: true,
					Event:   task.ConversationEvent{Type: ev.Type, Text: text, Time: ev.TS},
				})
			case "task.closed":
				var d struct {
					Reason string `json:"reason"`
				}
				_ = json.Unmarshal(ev.Data, &d)
				text := "Task closed"
				if d.Reason == "abandon" {
					text = "Task abandoned"
				}
				items = append(items, task.ConversationItem{
					IsEvent: true,
					Event:   task.ConversationEvent{Type: ev.Type, Text: text, Time: ev.TS},
				})
			case "stage.changed":
				var d struct {
					From string `json:"from"`
					To   string `json:"to"`
				}
				_ = json.Unmarshal(ev.Data, &d)
				var text string
				if d.From == "" {
					text = "Stage set to " + d.To
				} else {
					text = "Stage changed from " + d.From + " to " + d.To
				}
				items = append(items, task.ConversationItem{
					IsEvent: true,
					Event:   task.ConversationEvent{Type: ev.Type, Text: text, Time: ev.TS},
				})
			case "worker.started":
				items = append(items, task.ConversationItem{
					IsEvent: true,
					Event:   task.ConversationEvent{Type: ev.Type, Text: "Worker started", Time: ev.TS},
				})
			case "worker.finished":
				var d struct {
					Outcome      string `json:"outcome"`
					ToolCalls    int    `json:"tool_calls"`
					ErrorMessage string `json:"error_message"`
					Error        string `json:"error"`
				}
				_ = json.Unmarshal(ev.Data, &d)
				if strings.TrimSpace(d.ErrorMessage) == "" {
					d.ErrorMessage = d.Error
				}

				if strings.TrimSpace(d.Outcome) == "error" {
					text := "Worker error"
					if strings.TrimSpace(d.ErrorMessage) != "" {
						text += ": " + strings.TrimSpace(d.ErrorMessage)
					}
					if d.ToolCalls > 0 {
						text += fmt.Sprintf(" — %d tool calls", d.ToolCalls)
					}
					items = append(items, task.ConversationItem{
						IsEvent: true,
						Event:   task.ConversationEvent{Type: "worker.error", Text: text, Time: ev.TS},
					})
					break
				}

				text := "Worker finished"
				if d.Outcome != "" {
					text += " (" + d.Outcome + ")"
				}
				if d.ToolCalls > 0 {
					text += fmt.Sprintf(" — %d tool calls", d.ToolCalls)
				}
				items = append(items, task.ConversationItem{
					IsEvent: true,
					Event:   task.ConversationEvent{Type: ev.Type, Text: text, Time: ev.TS},
				})
			}
		}

		return conversationLoadedMsg{taskName: taskName, header: h, items: items, err: nil}
	}
}

func fetchDiffFilesCmd(taskName string, detail store.TaskView) tea.Cmd {
	return func() tea.Msg {
		ctx, err := computeDiffCtx(taskName, detail)
		if err != nil {
			return diffFilesLoadedMsg{taskName: taskName, err: err}
		}
		var files []git.DiffFileStat
		var status map[string]string
		if ctx.mode == diffModeWorkspace {
			files, err = git.DiffNumstat(ctx.dir, ctx.base)
			if err == nil {
				status, err = git.DiffNameStatus(ctx.dir, ctx.base)
			}
		} else {
			files, err = git.DiffNumstatRange(ctx.dir, ctx.base, ctx.branch)
			if err == nil {
				status, err = git.DiffNameStatusRange(ctx.dir, ctx.base, ctx.branch)
			}
		}
		if err == nil {
			for i := range files {
				if st, ok := status[files[i].Path]; ok {
					files[i].Status = st
				} else {
					files[i].Status = "M"
				}
			}
		}
		return diffFilesLoadedMsg{taskName: taskName, ctx: ctx, files: files, err: err}
	}
}

func computeDiffCtx(taskName string, detail store.TaskView) (diffCtx, error) {
	if detail.Task == nil {
		return diffCtx{}, fmt.Errorf("diff unavailable")
	}

	baseBranch := detail.Task.BaseBranch
	state := detail.State

	// Prefer diffing from the task workspace when available (includes uncommitted changes).
	if detail.TaskStatus == task.TaskStatusOpen && state != nil && state.Workspace != "" {
		if st, err := os.Stat(state.Workspace); err == nil && st.IsDir() {
			base, err := git.ResolveDiffBase(state.Workspace, "HEAD", baseBranch)
			if err != nil {
				return diffCtx{}, err
			}
			return diffCtx{mode: diffModeWorkspace, dir: state.Workspace, base: base}, nil
		}
	}

	repoDir := "."
	branch := taskName
	if git.BranchExists(repoDir, branch) {
		base, err := git.ResolveDiffBase(repoDir, branch, baseBranch)
		if err != nil {
			return diffCtx{}, err
		}
		return diffCtx{mode: diffModeRange, dir: repoDir, base: base, branch: branch}, nil
	}

	// Merged tasks: fall back to the squash merge commit.
	if detail.TaskStatus == task.TaskStatusMerged {
		tail, _ := history.Tail(taskName)
		if strings.TrimSpace(tail.LastMergedCommit) == "" {
			return diffCtx{}, fmt.Errorf("diff unavailable (missing merge commit)")
		}
		commit := strings.TrimSpace(tail.LastMergedCommit)
		return diffCtx{mode: diffModeRange, dir: repoDir, base: commit + "^", branch: commit}, nil
	}

	if detail.TaskStatus == task.TaskStatusOpen {
		return diffCtx{}, fmt.Errorf("task %s hasn't started yet (no branch)", taskName)
	}

	return diffCtx{}, fmt.Errorf("cannot diff %s: branch no longer exists", taskName)
}
