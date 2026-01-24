package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/store"
)

func TestStageText_HidesStageForClosedTasks(t *testing.T) {
	if got := stageText(store.TaskListItem{TaskStatus: task.TaskStatusClosed, Stage: "review"}); got != "review" {
		t.Fatalf("stageText(closed)=%q want %q", got, "review")
	}
	if got := stageText(store.TaskListItem{TaskStatus: task.TaskStatusOpen, Stage: "review"}); got != "review" {
		t.Fatalf("stageText(open)=%q want %q", got, "review")
	}
}

func TestProgressBar_RoundsAndClamps(t *testing.T) {
	tests := []struct {
		name  string
		done  int
		total int
		want  string
	}{
		{name: "total0", done: 1, total: 0, want: ""},
		{name: "negativeDone", done: -1, total: 10, want: strings.Repeat("─", 6)},
		{name: "zero", done: 0, total: 10, want: strings.Repeat("─", 6)},
		{name: "half", done: 5, total: 10, want: "━━━" + strings.Repeat("─", 3)},
		{name: "almostFull", done: 9, total: 10, want: "━━━━━" + "─"},
		{name: "full", done: 10, total: 10, want: strings.Repeat("━", 6)},
		{name: "overfull", done: 11, total: 10, want: strings.Repeat("━", 6)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ansi.Strip(progressBar(tt.done, tt.total))
			if got != tt.want {
				t.Fatalf("progressBar(%d,%d)=%q want %q", tt.done, tt.total, got, tt.want)
			}
		})
	}
}
