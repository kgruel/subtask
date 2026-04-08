package tui

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/kgruel/subtask/pkg/diffparse"
)

func fetchDiffDocCmd(taskName string, ctx diffCtx, path string, loadID int, cmdCtx context.Context) tea.Cmd {
	return func() tea.Msg {
		if path == "" || ctx.dir == "" || ctx.base == "" {
			return diffFileLoadedMsg{taskName: taskName, ctx: ctx, path: path, loadID: loadID, err: fmt.Errorf("diff unavailable")}
		}

		args := []string{"diff", "--no-color", "--no-ext-diff"}
		if ctx.mode == diffModeWorkspace {
			args = append(args, ctx.base, "--", path)
		} else {
			args = append(args, ctx.base+".."+ctx.branch, "--", path)
		}

		cmd := exec.CommandContext(cmdCtx, "git", args...)
		cmd.Dir = ctx.dir

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return diffFileLoadedMsg{taskName: taskName, ctx: ctx, path: path, loadID: loadID, err: err}
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr

		if err := cmd.Start(); err != nil {
			return diffFileLoadedMsg{taskName: taskName, ctx: ctx, path: path, loadID: loadID, err: err}
		}

		doc, parseErr := diffparse.ParseUnified(stdout)
		waitErr := cmd.Wait()

		if cmdCtx.Err() != nil {
			return nil
		}
		if parseErr != nil {
			return diffFileLoadedMsg{taskName: taskName, ctx: ctx, path: path, loadID: loadID, err: parseErr}
		}
		if waitErr != nil {
			if stderr.Len() > 0 {
				return diffFileLoadedMsg{taskName: taskName, ctx: ctx, path: path, loadID: loadID, err: fmt.Errorf("%w: %s", waitErr, stderr.String())}
			}
			return diffFileLoadedMsg{taskName: taskName, ctx: ctx, path: path, loadID: loadID, err: waitErr}
		}
		return diffFileLoadedMsg{taskName: taskName, ctx: ctx, path: path, loadID: loadID, doc: doc}
	}
}
