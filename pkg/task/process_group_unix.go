//go:build !windows

package task

import "syscall"

// SelfProcessGroupID returns the current process group ID, or 0 if unknown.
func SelfProcessGroupID() int {
	return syscall.Getpgrp()
}

// EnsureOwnProcessGroup puts the current process into its own process group
// (PGID == PID) so that interrupts can safely target the group without
// affecting unrelated processes.
//
// Best-effort: if it fails, callers should fall back to PID-only signaling.
func EnsureOwnProcessGroup() {
	_ = syscall.Setpgid(0, 0)
}
