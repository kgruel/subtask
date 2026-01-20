package main

import (
	"encoding/json"
	"strings"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
)

func sessionHarnessForTask(taskName string, st *task.State) string {
	if st != nil && strings.TrimSpace(st.Harness) != "" {
		return strings.TrimSpace(st.Harness)
	}

	// History fallback: last worker.session event.
	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	if err != nil {
		return ""
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type != "worker.session" {
			continue
		}
		var d struct {
			Harness string `json:"harness"`
		}
		if json.Unmarshal(evs[i].Data, &d) == nil && strings.TrimSpace(d.Harness) != "" {
			return strings.TrimSpace(d.Harness)
		}
	}
	return ""
}
