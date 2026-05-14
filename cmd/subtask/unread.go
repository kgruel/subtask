package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/index"
	"github.com/kgruel/subtask/pkg/workflow"
)

// UnreadCmd implements 'subtask unread' — lists open tasks where the most
// recent worker.finished event has no subsequent lead message. The Stop hook
// uses this to remind the lead before it ends a turn.
//
// Output: one task name per line, alphabetically sorted (via os.ReadDir order
// from task.List). Exit 0 if any unread, exit 1 if none — so callers can
// branch on `if subtask unread; then ...`.
type UnreadCmd struct{}

func (c *UnreadCmd) Run() error {
	if _, err := preflightProject(); err != nil {
		return err
	}

	names, err := openTaskNames()
	if err != nil {
		return err
	}

	any := false
	for _, name := range names {
		unread, err := taskHasUnreadReply(name)
		if err != nil {
			continue
		}
		if unread {
			fmt.Println(name)
			any = true
		}
	}

	if !any {
		os.Exit(1)
	}
	return nil
}

// openTaskNames returns names of tasks the index considers open. Refreshing
// first ensures recently closed/merged tasks (or task folders that were
// cleaned up out-of-band) are filtered out — task.List() reads disk-resident
// folders directly and would include orphans. This is the same view
// `subtask list` uses, just trimmed to open status.
func openTaskNames() ([]string, error) {
	idx, err := index.OpenDefault()
	if err != nil {
		return nil, err
	}
	defer idx.Close()

	ctx := context.Background()
	if err := idx.Refresh(ctx, index.RefreshPolicy{Git: index.GitPolicy{Mode: index.GitNone}}); err != nil {
		return nil, err
	}

	items, err := idx.ListOpen(ctx)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(items))
	for _, it := range items {
		if it.TaskStatus != task.TaskStatusOpen {
			continue
		}
		names = append(names, it.Name)
	}
	return names, nil
}

// taskHasUnreadReply returns true if the task is open and the most recent
// activity was a worker reply (worker.finished) with no lead message after it.
//
// Per-stage silence: when the stage active at WORKER REPLY TIME has
// `notify: false` (workflow stage) or `notify: false` / `surface: false`
// (routine step/terminal), the reply is treated as plumbing and
// suppressed from unread. The stage is the unit of policy — anything
// that happened while the task sat in a silent stage is silent.
//
// Critically, the policy is evaluated against the stage stamped on
// worker.finished, NOT the current tail.Stage. Routine auto-advance
// moves t.Stage AFTER worker.finished is appended, so reading the
// current tail.Stage would surface a silent reply as unread whenever
// the silent step has an advance: auto edge into a non-silent
// successor. Legacy worker.finished events (no stage field) fall back
// to tail.Stage — pre-step-4 history stays readable.
func taskHasUnreadReply(name string) (bool, error) {
	tail, err := history.Tail(name)
	if err != nil {
		return false, err
	}
	if tail.TaskStatus != task.TaskStatusOpen {
		return false, nil
	}

	evs, err := history.Read(name, history.ReadOptions{})
	if err != nil {
		return false, err
	}

	// Walk backwards once. Two kinds of "needs lead action" markers:
	//   - worker.finished: a worker just replied. Apply the silence
	//     policy against the stamped stage (or fall back to tail).
	//   - routine.surfaced: auto-advance landed on a surfaced gate or
	//     terminal step. No worker reply is coming — the routine
	//     handed off to the lead. Always surfaces (the runner only
	//     emits when IsSurfaced() == true).
	// A lead message AFTER either marker means the lead has touched
	// the task already; everything before is read.
	var (
		replyStage      string
		hasReplyStage   bool
		hasUnreadReply  bool
		hasSurfaceEvent bool
	)
	for i := len(evs) - 1; i >= 0; i-- {
		ev := evs[i]
		if ev.Type == "message" && ev.Role == "lead" {
			break
		}
		switch ev.Type {
		case "routine.surfaced":
			// Surfaced gate/terminal handoff. Nothing more recent
			// matters; the lead needs to act on this regardless of
			// any older worker.finished's silence policy.
			hasSurfaceEvent = true
		case "worker.finished":
			hasUnreadReply = true
			var d struct {
				Stage string `json:"stage"`
			}
			if err := json.Unmarshal(ev.Data, &d); err == nil && strings.TrimSpace(d.Stage) != "" {
				replyStage = d.Stage
				hasReplyStage = true
			}
		}
		if hasSurfaceEvent || hasUnreadReply {
			break
		}
	}

	if hasSurfaceEvent {
		return true, nil
	}
	if !hasUnreadReply {
		return false, nil
	}

	// Stage to evaluate the policy against: the stamped reply stage
	// wins; fall back to the current tail.Stage for legacy events.
	policyStage := tail.Stage
	if hasReplyStage {
		policyStage = replyStage
	}

	if policyStage != "" {
		t, _ := task.Load(name)
		if t != nil && t.Routine != "" {
			if r, _ := routine.LoadByName(t.Routine); r != nil {
				if s := r.GetStep(policyStage); s != nil {
					if s.IsSilent() {
						return false, nil
					}
					// `surface: false` opts a terminal OR gate step out of
					// unread surfacing. The schema (Step.Surface comment in
					// pkg/routine/routine.go) documents the field on both
					// kinds; an earlier version of this check only honored
					// it on terminals, missing the gate half.
					if (s.Kind == routine.KindTerminal || s.Kind == routine.KindGate) && !s.IsSurfaced() {
						return false, nil
					}
				}
			}
		} else if wf, _ := workflow.LoadFromTask(name); wf != nil {
			if wf.GetStage(policyStage).IsSilent() {
				return false, nil
			}
		}
	}

	return true, nil
}
