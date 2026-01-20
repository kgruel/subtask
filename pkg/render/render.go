// Package render provides CLI output rendering with automatic TTY detection.
// When stdout is a TTY (interactive terminal), output is pretty with colors and boxes.
// When stdout is not a TTY (piped, agents), output is plain text.
//
// Override with SUBTASK_OUTPUT=plain or SUBTASK_OUTPUT=pretty.
package render

import (
	"fmt"
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

// Output is the interface for types that can render themselves in both modes.
// Implement this for custom output types that need different plain/pretty rendering.
type Output interface {
	RenderPlain() string
	RenderPretty() string
}

// Render outputs an Output type using the appropriate mode.
func Render(o Output) {
	if Pretty {
		fmt.Print(o.RenderPretty())
	} else {
		fmt.Print(o.RenderPlain())
	}
}

// Renderln outputs an Output type with a trailing newline.
func Renderln(o Output) {
	Render(o)
	fmt.Println()
}
