package tui

import (
	"testing"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/store"
)

// TestArtifacts_ClampOnReload verifies that artifactSelected is clamped when the
// artifact list shrinks, and that render does not panic on the updated model.
func TestArtifacts_ClampOnReload(t *testing.T) {
	taskName := "fix/clamp-test"

	m := newModel()
	m.mode = viewDetail
	m.tab = tabArtifacts
	m.width = 120
	m.height = 40
	m.selectedTaskName = taskName
	m.detailTaskName = taskName
	m.tasks = []store.TaskListItem{{Name: taskName, TaskStatus: task.TaskStatusOpen}}

	// Start with 3 artifacts and cursor on the last one.
	m.artifacts = []task.ArtifactInfo{
		{Name: "a.md", Path: "a.md"},
		{Name: "b.md", Path: "b.md"},
		{Name: "c.md", Path: "c.md"},
	}
	m.artifactSelected = 2
	m.artifactViewMode = artifactModeList
	m.resize()

	// Reload with only 1 artifact — artifactSelected must clamp to 0.
	next, _ := m.Update(artifactsLoadedMsg{
		taskName:  taskName,
		artifacts: []task.ArtifactInfo{{Name: "a.md", Path: "a.md"}},
	})
	got := next.(model)

	if got.artifactSelected != 0 {
		t.Fatalf("expected artifactSelected=0 after clamp, got %d", got.artifactSelected)
	}

	// updateArtifactsContent must not panic on the clamped model.
	got.updateArtifactsContent()
}

// TestArtifacts_ViewModeDropsWhenSelectedGone verifies that view mode resets to
// list when the artifact being viewed is no longer present after a reload.
func TestArtifacts_ViewModeDropsWhenSelectedGone(t *testing.T) {
	taskName := "fix/view-drop-test"

	m := newModel()
	m.mode = viewDetail
	m.tab = tabArtifacts
	m.width = 120
	m.height = 40
	m.selectedTaskName = taskName
	m.detailTaskName = taskName
	m.tasks = []store.TaskListItem{{Name: taskName, TaskStatus: task.TaskStatusOpen}}

	m.artifacts = []task.ArtifactInfo{
		{Name: "a.md", Path: "a.md"},
		{Name: "b.md", Path: "b.md"},
		{Name: "c.md", Path: "c.md"},
	}
	m.artifactSelected = 2
	m.artifactViewMode = artifactModeView
	m.resize()

	// Reload shrinks list to 1 — index 2 is gone, so view mode must drop.
	next, _ := m.Update(artifactsLoadedMsg{
		taskName:  taskName,
		artifacts: []task.ArtifactInfo{{Name: "a.md", Path: "a.md"}},
	})
	got := next.(model)

	if got.artifactViewMode != artifactModeList {
		t.Fatalf("expected artifactModeList after selected artifact gone, got %d", got.artifactViewMode)
	}
	if got.artifactSelected != 0 {
		t.Fatalf("expected artifactSelected=0, got %d", got.artifactSelected)
	}
}
