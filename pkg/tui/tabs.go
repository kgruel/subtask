package tui

type tab int

const (
	tabOverview tab = iota
	tabConversation
	tabDiff
	tabConflicts // Only shown when conflicts exist
	tabCount
)

func (t tab) Title() string {
	switch t {
	case tabOverview:
		return "Overview"
	case tabConversation:
		return "Conversation"
	case tabDiff:
		return "Changes"
	case tabConflicts:
		return "⚠ Conflicts"
	default:
		return ""
	}
}
