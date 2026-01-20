//go:build windows

package main

import (
	"errors"
	"os"
)

func sendInterruptSignal(pid, _ int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(os.Interrupt)
}

func isNoSuchProcess(err error) bool {
	return errors.Is(err, os.ErrProcessDone)
}
