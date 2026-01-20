package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// Run launches the Subtask TUI.
func Run() error {
	recordStartup(time.Now())

	zone.NewGlobal()
	defer zone.Close()

	m := newModel()
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}
