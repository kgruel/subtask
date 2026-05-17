package tui

import "testing"

func TestTabSlotsStable(t *testing.T) {
	if tabOverview != tab(0) {
		t.Errorf("tabOverview = %d, want 0", tabOverview)
	}
	if tabConversation != tab(1) {
		t.Errorf("tabConversation = %d, want 1", tabConversation)
	}
	if tabArtifacts != tab(2) {
		t.Errorf("tabArtifacts = %d, want 2", tabArtifacts)
	}
	if tabDiff != tab(3) {
		t.Errorf("tabDiff = %d, want 3", tabDiff)
	}
	if tabConflicts != tab(4) {
		t.Errorf("tabConflicts = %d, want 4", tabConflicts)
	}
	if tabCount != tab(5) {
		t.Errorf("tabCount = %d, want 5", tabCount)
	}
}
