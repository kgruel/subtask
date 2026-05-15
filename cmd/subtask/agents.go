package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/kgruel/subtask/pkg/agent"
)

// AgentsCmd implements 'subtask agents'.
type AgentsCmd struct {
	JSON bool `help:"Machine-readable JSON output" short:"j"`
}

func (c *AgentsCmd) Run() error {
	res, err := preflightProject()
	if err != nil {
		return err
	}

	summaries, err := agent.List(res.Config)
	if err != nil {
		return err
	}

	if c.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summaries)
	}

	if len(summaries) == 0 {
		fmt.Println("No agents defined. Create one at .subtask/agents/<name>.yaml.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tPRESET\tPROMPT")
	for _, s := range summaries {
		presetCell := s.PresetLabel
		if !s.PresetValid {
			presetCell = "<missing>"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, presetCell, s.PromptSource)
	}
	return w.Flush()
}
