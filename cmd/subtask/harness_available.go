package main

import "github.com/kgruel/subtask/pkg/harness"

// isCommandAvailable checks if a command is likely runnable on this machine.
func isCommandAvailable(name string) bool {
	return harness.CanResolveCLI(name)
}

