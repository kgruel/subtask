package main

import (
	"fmt"

	"github.com/kgruel/subtask/pkg/task/ops"
)

// MergeCmd implements 'subtask merge'.
type MergeCmd struct {
	Task    string `arg:"" help:"Task name to merge"`
	Message string `short:"m" required:"" help:"Commit message for the squash commit"`
}

// Run executes the merge command.
func (c *MergeCmd) Run() error {
	if _, err := preflightProject(); err != nil {
		return err
	}

	res, err := ops.MergeTask(c.Task, c.Message, cliOpsLogger{})
	if err != nil {
		return err
	}
	// MergeTask returns exactly one of these for a no-op finalize; gate on
	// AlreadyMerged first so re-merging an already-merged task isn't silent.
	if res.AlreadyMerged {
		fmt.Printf("Task %s is already merged.\n", c.Task)
	} else if res.AlreadyClosed {
		fmt.Printf("Task %s is already closed (not merged).\n", c.Task)
	}
	return nil
}
