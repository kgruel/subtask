package main

import (
	"fmt"
	"strings"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
	"github.com/kgruel/subtask/pkg/task/migrate"
)

// ReplyCmd implements 'subtask reply' — prints the most recent worker message
// from history.jsonl as plain text. This is the canonical way for a lead to
// retrieve a worker's reply, durable against stdout capture/piping.
type ReplyCmd struct {
	Task string `arg:"" help:"Task name"`
}

func (c *ReplyCmd) Run() error {
	if _, err := preflightProject(); err != nil {
		return err
	}

	if err := migrate.EnsureSchema(c.Task); err != nil {
		return err
	}

	if _, err := task.Load(c.Task); err != nil {
		return fmt.Errorf("task %q not found", c.Task)
	}

	evs, err := history.Read(c.Task, history.ReadOptions{MessagesOnly: true})
	if err != nil {
		return err
	}

	for i := len(evs) - 1; i >= 0; i-- {
		ev := evs[i]
		if ev.Type != "message" || ev.Role != "worker" {
			continue
		}
		content := ev.Content
		fmt.Print(content)
		if !strings.HasSuffix(content, "\n") {
			fmt.Println()
		}
		return nil
	}

	return fmt.Errorf("no worker reply yet for task %q", c.Task)
}
