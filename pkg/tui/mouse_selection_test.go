package tui

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestStripZoneMarkers(t *testing.T) {
	in := "\x1b[12zhello\x1b[12z world \x1b[31mred\x1b[0m"
	out := stripZoneMarkers(in)
	if out != "hello world \x1b[31mred\x1b[0m" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestSelectionView_TextExtraction_Basic(t *testing.T) {
	view := "hello world"
	sel := uv.Rectangle{Min: uv.Pos(0, 0), Max: uv.Pos(5, 0)}
	sel.Max.Y++ // exclusive Y
	got := selectionView(view, 20, 1, sel, 0, 20, nil, true)
	if got != "hello" {
		t.Fatalf("got %q, want %q", got, "hello")
	}
}

func TestSelectionView_TextExtraction_RespectsBoundsMinX(t *testing.T) {
	view := "□ step one\n■ step two"
	sel := uv.Rectangle{Min: uv.Pos(2, 0), Max: uv.Pos(20, 1)}
	sel.Max.Y++ // select both lines
	got := selectionView(view, 20, 2, sel, 2, 20, nil, true)
	want := "step one\nstep two"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSelectionView_TextExtraction_PaneBoundsPreventsSidebarBleed(t *testing.T) {
	// Two columns in a single frame line: left sidebar + right content.
	// When selecting multiple lines in the right pane, bounds must constrain
	// selection expansion so it doesn't include the left pane on subsequent lines.
	view := strings.Join([]string{
		"file1.go  First line I want to copy",
		"file2.go  Second line too",
	}, "\n")

	rightStart := 10 // after "fileX.go  "
	rightEnd := 33

	sel := uv.Rectangle{Min: uv.Pos(rightStart, 0), Max: uv.Pos(rightEnd, 1)}
	sel.Max.Y++ // select both lines

	gotBleed := selectionView(view, 40, 2, sel, 0, 40, nil, true)
	if !strings.Contains(gotBleed, "file2.go") {
		t.Fatalf("expected bleed example to include sidebar text; got %q", gotBleed)
	}

	got := selectionView(view, 40, 2, sel, rightStart, rightEnd, nil, true)
	if strings.Contains(got, "file1.go") || strings.Contains(got, "file2.go") {
		t.Fatalf("expected constrained selection to exclude sidebar text; got %q", got)
	}
}
