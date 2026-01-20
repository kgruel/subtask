//go:build !windows

package main

import (
	"errors"
	"syscall"
)

func sendInterruptSignal(pid, pgid int) error {
	sig := syscall.SIGINT

	// Prefer the recorded process group for best-effort graceful cancellation
	// of both the supervisor and its harness subprocesses.
	if pgid != 0 {
		if err := syscall.Kill(-pgid, sig); err == nil {
			return nil
		}
	}

	// Fall back to trying the PID as a process group (common in interactive shells).
	if err := syscall.Kill(-pid, sig); err == nil {
		return nil
	}

	return syscall.Kill(pid, sig)
}

func isNoSuchProcess(err error) bool {
	return errors.Is(err, syscall.ESRCH)
}
