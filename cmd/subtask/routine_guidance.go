package main

import (
	"fmt"
	"strings"

	"github.com/kgruel/subtask/pkg/task"
)

// formatRoutineStepGuidance renders the lead-facing routine diagram and
// current-step instructions. It mirrors the passive output from subtask stage.
func formatRoutineStepGuidance(v *task.View, taskName string) string {
	if v == nil || v.Routine == nil || v.Routine.Instructions == "" {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Step: %s\n\n", v.Routine.Diagram)

	displayName := v.Routine.CurrentStep
	if displayName != "" {
		displayName = strings.ToUpper(displayName[:1]) + displayName[1:]
	}
	fmt.Fprintf(&b, "%s:\n", displayName)

	lines := strings.Split(strings.TrimSpace(v.Routine.Instructions), "\n")
	for _, line := range lines {
		line = strings.ReplaceAll(line, "<task>", taskName)
		fmt.Fprintf(&b, "  %s\n", line)
	}
	return b.String()
}
