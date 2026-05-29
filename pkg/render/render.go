// Package render provides CLI output rendering with automatic TTY detection.
// When stdout is a TTY (interactive terminal), output is pretty with colors and boxes.
// When stdout is not a TTY (piped, agents), output is plain text.
//
// Override with SUBTASK_OUTPUT=plain or SUBTASK_OUTPUT=pretty.
package render

import (
	"os"

	"github.com/mattn/go-isatty"
)

// Pretty indicates whether to use pretty (styled) output.
// Set automatically based on TTY detection, can be overridden via env.
var Pretty bool

func init() {
	switch os.Getenv("SUBTASK_OUTPUT") {
	case "plain":
		Pretty = false
	case "pretty":
		Pretty = true
	default:
		Pretty = isatty.IsTerminal(os.Stdout.Fd())
	}
}

