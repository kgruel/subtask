package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
)

type followUpSeed struct {
	FromSessionID string
	FromWorkspace string
	FromHarness   string
}

func resolveFollowUpSeed(projectAdapter, followUp string) (*followUpSeed, error) {
	followUp = strings.TrimSpace(followUp)
	if followUp == "" {
		return nil, nil
	}

	seed := &followUpSeed{}

	// First, try followUp as a task name.
	if st, err := task.LoadState(followUp); err == nil && st != nil {
		seed.FromSessionID = strings.TrimSpace(st.SessionID)
		seed.FromWorkspace = strings.TrimSpace(st.Workspace)
		seed.FromHarness = strings.TrimSpace(sessionHarnessForTask(followUp, st))
	}
	if strings.TrimSpace(seed.FromSessionID) == "" || strings.TrimSpace(seed.FromHarness) == "" {
		if sid, h := lastSessionFromHistory(followUp); strings.TrimSpace(sid) != "" {
			if strings.TrimSpace(seed.FromSessionID) == "" {
				seed.FromSessionID = strings.TrimSpace(sid)
			}
			if strings.TrimSpace(seed.FromHarness) == "" {
				seed.FromHarness = strings.TrimSpace(h)
			}
		}
	}

	// If followUp doesn't look like a task (no state/history), treat it as a raw session ID.
	if strings.TrimSpace(seed.FromSessionID) == "" {
		if _, err := task.Load(followUp); err != nil {
			seed.FromSessionID = followUp
			seed.FromHarness = strings.TrimSpace(projectAdapter)
		}
	}

	if strings.TrimSpace(seed.FromSessionID) == "" {
		return nil, fmt.Errorf("follow-up %q has no session to continue\n\nTip: Pass a harness session ID directly, or draft a new task with background context in the description.", followUp)
	}

	// Enforce harness compatibility when known.
	if strings.TrimSpace(seed.FromHarness) != "" && strings.TrimSpace(projectAdapter) != "" && seed.FromHarness != projectAdapter {
		return nil, fmt.Errorf("follow-up %q was last run with harness %q, but this project is configured for %q\n\n"+
			"Sessions are not compatible across harnesses.\n"+
			"Tip: run without --follow-up to start a fresh session.",
			followUp, seed.FromHarness, projectAdapter)
	}

	return seed, nil
}

func lastSessionFromHistory(taskName string) (sessionID, harness string) {
	evs, err := history.Read(taskName, history.ReadOptions{EventsOnly: true})
	if err != nil {
		return "", ""
	}
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Type != "worker.session" {
			continue
		}
		var d struct {
			SessionID string `json:"session_id"`
			Harness   string `json:"harness"`
		}
		if json.Unmarshal(evs[i].Data, &d) != nil {
			continue
		}
		if strings.TrimSpace(d.SessionID) == "" {
			continue
		}
		return strings.TrimSpace(d.SessionID), strings.TrimSpace(d.Harness)
	}
	return "", ""
}
