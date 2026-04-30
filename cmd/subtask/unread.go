package main

import (
	"fmt"
	"os"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
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

	names, err := task.List()
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

// taskHasUnreadReply returns true if the task is open and the most recent
// activity was a worker reply (worker.finished) with no lead message after it.
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
