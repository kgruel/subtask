package render

import (
	"strings"
	"testing"
)

// linearSteps builds a []DiagramStep for a simple linear routine:
// regular → regular → terminal.
func linearDefaultSteps() []DiagramStep {
	return []DiagramStep{
		{ID: "doing"},
		{ID: "review"},
		{ID: "ready", Terminal: true},
	}
}

func theyPlanSteps() []DiagramStep {
	return []DiagramStep{
		{ID: "plan"},
		{ID: "implement"},
		{ID: "review"},
		{ID: "ready", Terminal: true},
	}
}

// fancySteps builds the non-trivial example from the PLAN:
// investigate(branch→deepdive) → deepdive → decide(gate: ship→done, retry↩investigate) → done(terminal)
func fancySteps() []DiagramStep {
	return []DiagramStep{
		{
			ID: "investigate",
			Edges: []DiagramEdge{
				{Label: "needs_deepdive", Target: "deepdive", Loopback: false},
			},
		},
		{ID: "deepdive"},
		{
			ID:   "decide",
			Gate: true,
			Edges: []DiagramEdge{
				{Label: "ship", Target: "done", Loopback: false},
				{Label: "retry", Target: "investigate", Loopback: true},
			},
		},
		{ID: "done", Terminal: true},
	}
}

func TestFormatRoutineDiagram_Linear_Default(t *testing.T) {
	Pretty = false
	defer func() { Pretty = false }()

	got := FormatRoutineDiagram(linearDefaultSteps(), "doing")
	want := "(doing) → review → ready!"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("linear routine must not produce flow notes; got:\n%s", got)
	}
}

func TestFormatRoutineDiagram_Linear_TheyPlan(t *testing.T) {
	Pretty = false
	defer func() { Pretty = false }()

	got := FormatRoutineDiagram(theyPlanSteps(), "implement")
	want := "plan → (implement) → review → ready!"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("linear routine must not produce flow notes; got:\n%s", got)
	}
}

func TestFormatRoutineDiagram_NonLinear(t *testing.T) {
	Pretty = false
	defer func() { Pretty = false }()

	got := FormatRoutineDiagram(fancySteps(), "decide")
	lines := strings.Split(got, "\n")

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (chain + 2 flow notes), got %d:\n%s", len(lines), got)
	}

	// Main chain: sigils present, current step in parens.
	wantChain := "investigate? → deepdive → (decide)* → done!"
	if lines[0] != wantChain {
		t.Errorf("chain: got %q, want %q", lines[0], wantChain)
	}

	// Branch flow note — forward edge, no ↩.
	if !strings.Contains(lines[1], "? investigate:") {
		t.Errorf("flow note 1 should reference investigate; got %q", lines[1])
	}
	if !strings.Contains(lines[1], "needs_deepdive → deepdive") {
		t.Errorf("flow note 1 should show forward edge; got %q", lines[1])
	}
	if strings.Contains(lines[1], "↩") {
		t.Errorf("flow note 1 must not contain loopback marker; got %q", lines[1])
	}

	// Gate flow note — loopback edge gets ↩.
	if !strings.Contains(lines[2], "* decide:") {
		t.Errorf("flow note 2 should reference decide; got %q", lines[2])
	}
	if !strings.Contains(lines[2], "ship → done") {
		t.Errorf("flow note 2 should show forward edge; got %q", lines[2])
	}
	if !strings.Contains(lines[2], "retry ↩ investigate") {
		t.Errorf("flow note 2 should show loopback with ↩; got %q", lines[2])
	}
}

func TestFormatRoutineDiagram_NoAnsiInPlainMode(t *testing.T) {
	Pretty = false
	defer func() { Pretty = false }()

	got := FormatRoutineDiagram(fancySteps(), "investigate")
	if strings.ContainsRune(got, '\x1b') {
		t.Errorf("plain mode output must not contain ANSI sequences; got:\n%s", got)
	}
}

// TestFormatRoutineDiagram_BranchSourceIsLoopbackTarget verifies that when a
// step is both a branch source (it has edges) and a loopback target (another
// step routes back to it), the flow notes are correct:
//   - investigate's note shows its own branch (forward edge, no ↩)
//   - decide's note shows the loopback back to investigate (↩ present)
func TestFormatRoutineDiagram_BranchSourceIsLoopbackTarget(t *testing.T) {
	Pretty = false
	defer func() { Pretty = false }()

	// Same as fancySteps but current="" to keep the chain clean.
	got := FormatRoutineDiagram(fancySteps(), "")
	lines := strings.Split(got, "\n")

	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), got)
	}

	investigateNote := lines[1]
	if !strings.Contains(investigateNote, "? investigate:") {
		t.Errorf("line 1 should be investigate's branch note; got %q", investigateNote)
	}
	// investigate's own edges are forward — must NOT have ↩.
	if strings.Contains(investigateNote, "↩") {
		t.Errorf("investigate note must not contain ↩ (it's a forward-edge source); got %q", investigateNote)
	}

	decideNote := lines[2]
	if !strings.Contains(decideNote, "retry ↩ investigate") {
		t.Errorf("decide note must show loopback; got %q", decideNote)
	}
}

func TestFormatRoutineDiagram_Empty(t *testing.T) {
	Pretty = false
	got := FormatRoutineDiagram(nil, "any")
	if got != "" {
		t.Errorf("nil steps: got %q, want empty", got)
	}
	got = FormatRoutineDiagram([]DiagramStep{}, "any")
	if got != "" {
		t.Errorf("empty steps: got %q, want empty", got)
	}
}

func TestFormatRoutineDiagram_NoCurrent(t *testing.T) {
	Pretty = false
	defer func() { Pretty = false }()

	got := FormatRoutineDiagram(linearDefaultSteps(), "")
	want := "doing → review → ready!"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestFormatRoutineDiagram_SelfLoop verifies that a branch/option whose target
// is the same step (index == source index) renders as a loopback (↩), not a
// forward edge (→). Self-loops are retry edges and must not be misread as
// forward progress.
func TestFormatRoutineDiagram_SelfLoop(t *testing.T) {
	Pretty = false
	defer func() { Pretty = false }()

	steps := []DiagramStep{
		{
			ID: "plan",
			// Two edges: self-loop (Loopback: true) and a forward edge.
			Edges: []DiagramEdge{
				{Label: "needs_revision", Target: "plan", Loopback: true},
				{Label: "approved", Target: "implement", Loopback: false},
			},
		},
		{ID: "implement"},
		{ID: "done", Terminal: true},
	}

	got := FormatRoutineDiagram(steps, "plan")
	lines := strings.Split(got, "\n")

	// Chain: current step with branch sigil.
	if !strings.Contains(lines[0], "(plan)?") {
		t.Errorf("chain should show branch sigil on current plan step; got %q", lines[0])
	}

	// Flow note: exactly one note line for plan.
	if len(lines) < 2 {
		t.Fatalf("expected flow note line; got:\n%s", got)
	}
	note := lines[1]
	if !strings.Contains(note, "? plan:") {
		t.Errorf("flow note should reference plan; got %q", note)
	}
	// Self-loop must use ↩.
	if !strings.Contains(note, "needs_revision ↩ plan") {
		t.Errorf("self-loop must render with ↩; got %q", note)
	}
	// Forward edge must use →.
	if !strings.Contains(note, "approved → implement") {
		t.Errorf("forward edge must render with →; got %q", note)
	}
}
