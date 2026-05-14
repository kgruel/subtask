package main

import (
	"context"
	"fmt"
	"os"

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
// Per-stage silence: when the task's current stage has `notify: false` in the
// workflow YAML, any worker reply in that stage is treated as plumbing and
// suppressed from unread. The stage is the unit of policy — anything that
// happens while the task sits in a silent stage is silent.
func taskHasUnreadReply(name string) (bool, error) {
	tail, err := history.Tail(name)
	if err != nil {
		return false, err
	}
	if tail.TaskStatus != task.TaskStatusOpen {
		return false, nil
	}

	if tail.Stage != "" {
		if wf, _ := workflow.LoadFromTask(name); wf != nil {
			if wf.GetStage(tail.Stage).IsSilent() {
				return false, nil
			}
		}
	}

	evs, err := history.Read(name, history.ReadOptions{})
	if err != nil {
		return false, err
	}

	for i := len(evs) - 1; i >= 0; i-- {
		ev := evs[i]
		switch {
		case ev.Type == "worker.finished":
			return true, nil
		case ev.Type == "message" && ev.Role == "lead":
			return false, nil
		}
	}
	return false, nil
}
