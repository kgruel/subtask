//go:build windows

// Package detach configures a command to run as a detached background process
// that outlives the launching shell.
package detach

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// Detached configures cmd to run as a detached background process with its own
// process group. DETACHED_PROCESS gives it no console at all, so it cannot be
// reached by console-Ctrl interrupts (a pre-existing Windows interrupt limit).
func Detached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
}
