package render

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// padRight pads a string to the given display width, accounting for ANSI escapes
// and wide runes via ansi.StringWidth (never byte len()).
func padRight(s string, width int) string {
	dw := ansi.StringWidth(s)
	if dw >= width {
		return s
	}
	return s + strings.Repeat(" ", width-dw)
}

// Write is a helper for writing styled output to a writer.
func Write(w *os.File, s string) {
	fmt.Fprint(w, s)
}
