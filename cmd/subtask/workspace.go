package main

import (
	"fmt"

	"github.com/zippoxer/subtask/pkg/workspace"
)

// WorkspaceCmd implements 'subtask workspace'.
type WorkspaceCmd struct {
	Task string `arg:"" help:"Task name"`
}

// Run executes the workspace command.
func (c *WorkspaceCmd) Run() error {
	ws, err := workspace.ForTask(c.Task)
	if err != nil {
		return err
	}
	fmt.Println(ws)
	return nil
}
