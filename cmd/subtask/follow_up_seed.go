package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
)

type followUpSeed struct {
	FromSessionID string
	FromWorkspace string
	FromHarness   string

	// IncompatibleParentHarness, when non-empty, records that the seed degraded
	// to artifact-only continuity because the parent's session harness differs
	// from the target adapter. It names the parent's original harness so
	// send.go can warn about the mismatch. FromSessionID/FromHarness are
	// cleared in this case so no session resume is attempted; ## Parent
	// Context (keyed on t.FollowUp) carries continuity forward.
	IncompatibleParentHarness string

	// ParentWorkspaceLive records whether the parent's workspace still existed
	// on disk at resolution time. It only matters for messaging: send.go uses
	// it to distinguish "parent is live but adapters differ" from "parent was
	// merged/closed" in the continuity-downgrade warning.
	ParentWorkspaceLive bool
}

// workspaceIsLive reports whether path is non-empty and still exists on disk.
// A stale FromWorkspace (deleted worktree, gc'd path) counts as not-live.
func workspaceIsLive(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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

	// No resolvable session. If followUp is a real task, the raw-session
	// fallback above did not fire (it only fires when task.Load fails), so this
	// is a never-dispatched parent used purely as an artifact container. That is
	// valid artifacts-first continuity: there is no session to resume, but its
	// TASK.md/PLAN.md/PROGRESS.json/produces files are injected as ## Parent
	// Context by BuildPrompt. Return an artifact-only seed rather than failing.
	if strings.TrimSpace(seed.FromSessionID) == "" {
		if _, err := task.Load(followUp); err != nil {
			return nil, fmt.Errorf("follow-up %q has no session to continue\n\nTip: Pass a session ID directly, or draft a new task with background context in the description.", followUp)
		}
		return seed, nil // real task, never sent → artifact-only continuity
	}

	// Enforce adapter compatibility when known.
	if strings.TrimSpace(seed.FromHarness) != "" && strings.TrimSpace(projectAdapter) != "" && seed.FromHarness != projectAdapter {
		// A session recorded by one adapter can never be resumed by another
		// (claude's session files and codex's session store are incompatible
		// formats), regardless of whether the parent workspace is still live.
		// Degrade to artifact-only continuity rather than hard-failing: record
		// the parent's original harness (for send.go's mismatch warning) and
		// clear the session so no cross-adapter resume is attempted; ##
		// Parent Context carries continuity forward. This is what makes the
		// documented cross-family aggregation flow (e.g. claude parent →
		// codex child) work — the follow-up never fails for lack of a
		// resumable session, whether the parent is live, merged, or closed.
		seed.ParentWorkspaceLive = workspaceIsLive(seed.FromWorkspace)
		seed.IncompatibleParentHarness = seed.FromHarness
		seed.FromSessionID = ""
		seed.FromHarness = ""
		seed.FromWorkspace = ""
		return seed, nil
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
