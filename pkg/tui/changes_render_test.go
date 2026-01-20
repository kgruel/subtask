package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/zippoxer/subtask/pkg/diffparse"
)

func TestRenderUnifiedRow_EmptyAddedLineKeepsFullWidth(t *testing.T) {
	row := diffparse.UnifiedRow{Kind: diffparse.KindAdd, OldLine: 0, NewLine: 1, Text: ""}
	out := renderUnifiedRowLines(row, 80, 4, 10, 0, 10)
	if len(out) != 1 {
		t.Fatalf("lines=%d want=%d", len(out), 1)
	}

	if ansi.StringWidth(out[0]) != 80 {
		t.Fatalf("width=%d want=%d", ansi.StringWidth(out[0]), 80)
	}
	if !containsLiteral(out[0], "+") {
		t.Fatalf("expected '+' marker in output")
	}
}

func TestRenderUnifiedRow_WrapsWithBlankGutter(t *testing.T) {
	// width=20, lineNoW=4 => gutter=5, codeW=15, wrapW=13 (codeW-2)
	row := diffparse.UnifiedRow{Kind: diffparse.KindAdd, OldLine: 0, NewLine: 9999, Text: strings.Repeat("a", 30)}
	out := renderUnifiedRowLines(row, 20, 4, 13, 0, 10)
	if len(out) < 2 {
		t.Fatalf("lines=%d want>=2", len(out))
	}

	first := ansi.Strip(out[0])
	second := ansi.Strip(out[1])

	if !strings.Contains(first, "9999") {
		t.Fatalf("expected first line to include line number; got %q", first)
	}
	if strings.Contains(second, "9999") {
		t.Fatalf("expected continuation line to have blank gutter; got %q", second)
	}

	// Continuation line: blank gutter + 2-space continuation prefix.
	wantSpaces := 4 + 1 + 2
	if leadingSpaces(second) != wantSpaces {
		t.Fatalf("leadingSpaces=%d want=%d (%q)", leadingSpaces(second), wantSpaces, second)
	}
	if ansi.StringWidth(out[1]) != 20 {
		t.Fatalf("width=%d want=%d", ansi.StringWidth(out[1]), 20)
	}
}

func containsLiteral(s, lit string) bool {
	for i := 0; i+len(lit) <= len(s); i++ {
		if s[i:i+len(lit)] == lit {
			return true
		}
	}
	return false
}

func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}
