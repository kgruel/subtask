package render

import "github.com/charmbracelet/lipgloss"

// Color palette
var (
	colorGreen  = lipgloss.Color("42")
	colorYellow = lipgloss.Color("214")
	colorRed    = lipgloss.Color("196")
	colorBlue   = lipgloss.Color("39")
	colorGray   = lipgloss.Color("245")
	colorDim    = lipgloss.Color("246") // 240 is too dark on many dark themes
	colorPurple = lipgloss.Color("141") // For merged status
)

// Text styles
var (
	styleBold      = lipgloss.NewStyle().Bold(true)
	styleDim       = lipgloss.NewStyle().Foreground(colorDim)
	styleSuccess   = lipgloss.NewStyle().Foreground(colorGreen)
	styleWarning   = lipgloss.NewStyle().Foreground(colorYellow)
	styleError     = lipgloss.NewStyle().Foreground(colorRed)
	styleHighlight = lipgloss.NewStyle().Foreground(colorBlue)
)

// Status styles
var (
	styleStatusWorking = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	styleStatusReplied = lipgloss.NewStyle().Foreground(colorYellow)
	styleStatusError   = lipgloss.NewStyle().Foreground(colorRed)
	styleStatusDraft   = lipgloss.NewStyle().Foreground(colorGray)
	styleStatusClosed  = lipgloss.NewStyle().Foreground(colorDim)
	styleStatusMerged  = lipgloss.NewStyle().Foreground(colorPurple)
)

// Box styles
var (
	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorGray).
			Padding(0, 1).
			Margin(1, 0, 1, 1) // top, right, bottom, left

	styleBoxTitle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBlue).
			Padding(0, 1).
			Margin(1, 0, 1, 1)
)

// Table header style - no foreground color so it adapts to light/dark terminals
var styleTableHeader = lipgloss.NewStyle().Bold(true)
