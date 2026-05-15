package main

import (
	"github.com/kgruel/subtask/pkg/render"
	"github.com/kgruel/subtask/pkg/routine"
)

// routineDiagramSteps converts a *routine.Routine to []render.DiagramStep for
// use with render.FormatRoutineDiagram. Loopback edges are detected by step
// position: a target whose index is ≤ the source's index is a loopback
// (includes self-loops where target == source).
func routineDiagramSteps(r *routine.Routine) []render.DiagramStep {
	if r == nil {
		return nil
	}

	idxOf := make(map[string]int, len(r.Steps))
	for i, s := range r.Steps {
		idxOf[s.ID] = i
	}

	steps := make([]render.DiagramStep, len(r.Steps))
	for i, s := range r.Steps {
		ds := render.DiagramStep{
			ID:       s.ID,
			Terminal: s.Kind == routine.KindTerminal,
			Gate:     s.Kind == routine.KindGate,
		}
		switch s.Kind {
		case routine.KindGate:
			ds.Edges = make([]render.DiagramEdge, len(s.Options))
			for j, o := range s.Options {
				ds.Edges[j] = render.DiagramEdge{
					Label:    o.Name,
					Target:   o.To,
					Loopback: idxOf[o.To] <= i,
				}
			}
		default:
			if len(s.Branches) > 0 {
				ds.Edges = make([]render.DiagramEdge, len(s.Branches))
				for j, b := range s.Branches {
					ds.Edges[j] = render.DiagramEdge{
						Label:    b.Field,
						Target:   b.To,
						Loopback: idxOf[b.To] <= i,
					}
				}
			}
		}
		steps[i] = ds
	}
	return steps
}
