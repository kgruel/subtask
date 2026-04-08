package main

import (
	"fmt"
	"strings"

	"github.com/kgruel/subtask/pkg/task"
)

func enforceTaskHarnessMatch(taskName string, st *task.State, projectAdapter string) error {
	if st == nil {
		return nil
	}
	if strings.TrimSpace(projectAdapter) == "" || strings.TrimSpace(st.Adapter) == "" {
		return nil
	}
	if st.Adapter == projectAdapter {
		return nil
	}
	return fmt.Errorf("task %q was last run with harness %q, but this project is configured for %q\n\n"+
		"Sessions are not compatible across harnesses.\n"+
		"Tip: run without --follow-up to start a fresh session.",
		taskName, st.Adapter, projectAdapter)
}
