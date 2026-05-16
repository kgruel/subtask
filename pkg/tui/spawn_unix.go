//go:build !windows

package tui

import (
	"os/exec"
	"syscall"
)

func setDetachedProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
