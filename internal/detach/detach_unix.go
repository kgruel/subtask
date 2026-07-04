//go:build !windows

// Package detach configures a command to run as a detached background process
// that outlives the launching shell.
package detach

import (
	"os/exec"
	"syscall"
)

// Detached configures cmd to run as a session-detached background process: a
// new session (setsid) gives it its own process group (PGID == child PID), no
// controlling terminal, and immunity to SIGHUP when the launching shell exits.
//
// Setsid (not the TUI's Setpgid) is deliberate: Setpgid leaves the child in the
// launching shell's session, so it takes SIGHUP when the terminal closes. A
// supervisor whose purpose is to outlive the lead's shell must be a new session
// leader. Do not also set Setpgid — that would move the child out of the group
// it just created.
func Detached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
