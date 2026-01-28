package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zippoxer/subtask/pkg/task"
	"github.com/zippoxer/subtask/pkg/task/history"
	"github.com/zippoxer/subtask/pkg/task/migrate"
)

// WaitCmd implements 'subtask wait'.
type WaitCmd struct {
	Task string `arg:"" help:"Task name"`
}

func (c *WaitCmd) Run() error {
	if _, err := preflightProject(); err != nil {
		return err
	}

	// Ensure schema/history exist (one-time) and task exists.
	if err := migrate.EnsureSchema(c.Task); err != nil {
		return err
	}
	if _, err := task.Load(c.Task); err != nil {
		return fmt.Errorf("task %q not found\n\nCreate it first:\n  subtask draft %s --base-branch <branch> --title \"...\"",
			c.Task, c.Task)
	}

	st, err := task.LoadState(c.Task)
	if err != nil {
		return err
	}
	if st == nil || strings.TrimSpace(st.OutputPath) == "" {
		return fmt.Errorf("task %s has no async output file recorded", c.Task)
	}
	outputPath := strings.TrimSpace(st.OutputPath)

	pid := 0
	if st != nil {
		pid = st.SupervisorPID
	}

	if pid != 0 {
		latched := pid
		for task.ProcessAlive(latched) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	tail, _ := history.Tail(c.Task)
	fmt.Println(outputPath)

	if strings.TrimSpace(tail.LastRunOutcome) == "replied" {
		os.Exit(0)
	}
	os.Exit(1)
	return nil
}
