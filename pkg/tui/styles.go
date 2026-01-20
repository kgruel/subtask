package tui

import "github.com/charmbracelet/lipgloss"

// Colors matching render package
var (
	colorGreen  = lipgloss.Color("42")
	colorYellow = lipgloss.Color("214")
	colorRed    = lipgloss.Color("196")
	colorPurple = lipgloss.Color("141")
	colorBlue   = lipgloss.Color("75")
	colorDim    = lipgloss.Color("246")
)

var (
	styleBold = lipgloss.NewStyle().Bold(true)

	// Selection indicator for list view
	styleSelectionIndicator = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)
	styleSelectedTaskName   = lipgloss.NewStyle().Foreground(colorBlue).Bold(true)

	styleDim = lipgloss.NewStyle().Foreground(colorDim)

	// Table header - bold, no color (adapts to terminal)
	styleTableHeader = lipgloss.NewStyle().Bold(true)

	styleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.AdaptiveColor{Light: "63", Dark: "63"}).
			Padding(0, 1)
	styleTabInactive = lipgloss.NewStyle().
				Padding(0, 1)

	// Status colors
	styleStatusWorking     = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	styleStatusReplied     = lipgloss.NewStyle().Foreground(colorYellow)
	styleStatusError       = lipgloss.NewStyle().Foreground(colorRed)
	styleStatusInterrupted = lipgloss.NewStyle().Foreground(colorYellow)
	styleStatusDraft       = lipgloss.NewStyle().Foreground(colorBlue)
	styleStatusClosed      = lipgloss.NewStyle().Foreground(colorDim)
	styleStatusMerged      = lipgloss.NewStyle().Foreground(colorPurple)

	// Changes column colors
	styleSuccess = lipgloss.NewStyle().Foreground(colorGreen)
	styleError   = lipgloss.NewStyle().Foreground(colorRed)

	// Keycap style - subtle background for keyboard shortcuts
	styleKeycap = lipgloss.NewStyle().
			Background(lipgloss.AdaptiveColor{Light: "252", Dark: "238"}).
			Padding(0, 1)

	// Changes (diffnav-inspired) styles
	styleChangesGutter = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "240"})
	styleChangesSep = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "245", Dark: "240"})
	styleChangesHunk = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "242", Dark: "240"})
	styleChangesPlus = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "0", Dark: "15"}).
				Background(lipgloss.AdaptiveColor{Light: "194", Dark: "22"})
	styleChangesMinus = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "0", Dark: "15"}).
				Background(lipgloss.AdaptiveColor{Light: "224", Dark: "52"})

	styleChangesTreeDir = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "242", Dark: "245"})
	styleChangesTreeSelected = lipgloss.NewStyle().
					Bold(true).
					Background(lipgloss.AdaptiveColor{Light: "252", Dark: "237"})
)
