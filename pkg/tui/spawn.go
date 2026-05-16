package tui

import (
	"bytes"
	"io"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type spawnStartedMsg struct {
	action   string
	taskName string
}

type spawnFailedMsg struct {
	action   string
	taskName string
	err      error
}

type spawnExitedMsg struct {
	action   string
	taskName string
	err      string
}

type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{limit: limit}
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	originalLen := len(p)
	if b.limit <= 0 || b.buf.Len() >= b.limit {
		return originalLen, nil
	}
	remaining := b.limit - b.buf.Len()
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, _ = b.buf.Write(p)
	return originalLen, nil
}

func (b *cappedBuffer) String() string {
	return b.buf.String()
}

func newSubtaskSpawner(binaryPath string) subtaskSpawner {
	return func(taskName string, args []string) tea.Cmd {
		return spawnSubtask(binaryPath, taskName, args)
	}
}

func spawnSubtask(binaryPath, taskName string, args []string) tea.Cmd {
	return func() tea.Msg {
		action := ""
		if len(args) > 0 {
			action = args[0]
		}

		cmd := exec.Command(binaryPath, args...)
		cmd.Stdout = io.Discard
		stderr := newCappedBuffer(8 * 1024)
		cmd.Stderr = stderr
		setDetachedProc(cmd)

		if err := cmd.Start(); err != nil {
			return spawnFailedMsg{action: action, taskName: taskName, err: err}
		}

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		select {
		case err := <-done:
			if err != nil {
				msg := strings.TrimSpace(stderr.String())
				if msg == "" {
					msg = err.Error()
				}
				return spawnExitedMsg{action: action, taskName: taskName, err: msg}
			}
			return spawnStartedMsg{action: action, taskName: taskName}
		case <-time.After(500 * time.Millisecond):
			go func() { <-done }()
			return spawnStartedMsg{action: action, taskName: taskName}
		}
	}
}

func tailRunes(s string, limit int) string {
	r := []rune(s)
	if limit <= 0 || len(r) <= limit {
		return s
	}
	return string(r[len(r)-limit:])
}

func truncateRunes(s string, limit int) string {
	r := []rune(s)
	if limit <= 0 || len(r) <= limit {
		return s
	}
	return string(r[:limit])
}

func titleCase(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}
