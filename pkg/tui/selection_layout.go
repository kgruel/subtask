package tui

// overviewSelectionLayout captures enough layout information to derive
// pane-aware selection bounds for the Overview tab.
type overviewSelectionLayout struct {
	leftW  int
	rightW int
	gapW   int
}
