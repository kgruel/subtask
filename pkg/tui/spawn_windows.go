//go:build windows

package tui

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

func setDetachedProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
}
