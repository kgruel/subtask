package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
)

const watchRefreshInterval = 2 * time.Second

func runWatch(render func() (string, error)) error {
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		return fmt.Errorf("--watch is only supported when stdout is a TTY")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if _, err := os.Stdout.WriteString("\x1b[?25l"); err != nil { // hide cursor
		return err
	}
	defer func() {
		_, _ = os.Stdout.WriteString("\x1b[?25h") // show cursor
	}()

	var previousLines int

	renderOnce := func() error {
		out, err := render()
		if err != nil {
			return err
		}
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}

		var buf strings.Builder
		if previousLines > 0 {
			fmt.Fprintf(&buf, "\x1b[%dA\r\x1b[J", previousLines) // cursor up, CR, clear below
		}
		buf.WriteString(out)

		if _, err := os.Stdout.WriteString(buf.String()); err != nil {
			return err
		}

		previousLines = strings.Count(out, "\n")
		return nil
	}

	if err := renderOnce(); err != nil {
		return err
	}

	ticker := time.NewTicker(watchRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := renderOnce(); err != nil {
				return err
			}
		}
	}
}
