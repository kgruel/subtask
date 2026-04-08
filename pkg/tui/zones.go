package tui

import (
	"fmt"
	"strings"

	"github.com/kgruel/subtask/pkg/task"
)

func zoneTaskRow(taskName string) string {
	return "task-row:" + task.EscapeName(taskName)
}

func zoneTaskBadge(taskName string) string {
	return "task-badge:" + task.EscapeName(taskName)
}

func zoneTab(t tab) string {
	return fmt.Sprintf("tab:%d", int(t))
}

func zoneOverviewPane() string     { return "pane:overview" }
func zoneConversationPane() string { return "pane:conversation" }
func zoneDiffFilesPane() string    { return "pane:diff-files" }
func zoneDiffCodePane() string     { return "pane:diff-code" }

func zoneDiffFile(path string) string {
	return "diff-file:" + escapeZoneID(path)
}

func escapeZoneID(s string) string {
	r := strings.NewReplacer(
		"\n", "_",
		"\r", "_",
		"\t", "_",
		" ", "_",
		"/", "--",
		":", "_",
	)
	return r.Replace(s)
}
