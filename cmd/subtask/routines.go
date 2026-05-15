package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/kgruel/subtask/pkg/routine"
)

// RoutinesCmd implements 'subtask routines'.
type RoutinesCmd struct {
	JSON bool `help:"Machine-readable JSON output" short:"j"`
}

func (c *RoutinesCmd) Run() error {
	if _, err := preflightProjectOnly(); err != nil {
		return err
	}

	summaries, err := routine.List()
	if err != nil {
		return err
	}

	if c.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summaries)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NAME\tSOURCE\tENTRY\tTERMINALS\tDESCRIPTION")
	for _, s := range summaries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			s.Name, s.Source, s.EntryStep,
			strings.Join(s.TerminalSteps, ","),
			s.Description)
	}
	return w.Flush()
}
