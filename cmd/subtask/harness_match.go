package main

import (
	"fmt"
	"strings"

	"github.com/zippoxer/subtask/pkg/task"
)

func enforceTaskHarnessMatch(taskName string, st *task.State, projectHarness string) error {
	if st == nil {
		return nil
	}
	if strings.TrimSpace(projectHarness) == "" || strings.TrimSpace(st.Harness) == "" {
		return nil
	}
	if st.Harness == projectHarness {
		return nil
	}
	return fmt.Errorf("task %q was last run with harness %q, but this project is configured for %q\n\n"+
		"Sessions are not compatible across harnesses.\n"+
		"Tip: run without --follow-up to start a fresh session.",
		taskName, st.Harness, projectHarness)
}
