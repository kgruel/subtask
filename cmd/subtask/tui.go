package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/zippoxer/subtask/pkg/task"
	subtasktui "github.com/zippoxer/subtask/pkg/tui"
)

func runTUIWithInitCheck() error {
	if _, err := os.Stat(task.ConfigPath()); err != nil {
		if !os.IsNotExist(err) {
			return err
		}

		if !isatty.IsTerminal(os.Stdin.Fd()) || !isatty.IsTerminal(os.Stdout.Fd()) {
			return fmt.Errorf("subtask not initialized\n\nRun: subtask init")
		}

		fmt.Print("Subtask is not initialized. Run 'subtask init' now? (y/n): ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))
		if line != "y" && line != "yes" {
			return fmt.Errorf("subtask not initialized\n\nRun: subtask init")
		}

		if err := (&InitCmd{Workspaces: 20}).Run(); err != nil {
			return err
		}
	}

	return subtasktui.Run()
}
