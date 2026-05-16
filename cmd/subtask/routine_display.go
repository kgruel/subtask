package main

import (
	"github.com/kgruel/subtask/pkg/render"
	"github.com/kgruel/subtask/pkg/routine"
	"github.com/kgruel/subtask/pkg/task"
)

// routineDiagramSteps converts []task.StepView to []render.DiagramStep for
// use with render.FormatRoutineDiagram. Loopback edges are detected by step
// position: a target whose index is ≤ the source's index is a loopback
// (includes self-loops where target == source).
func routineDiagramSteps(steps []task.StepView) []render.DiagramStep {
	if len(steps) == 0 {
		return nil
	}

	idxOf := make(map[string]int, len(steps))
	for i, s := range steps {
		idxOf[s.ID] = i
	}

	out := make([]render.DiagramStep, len(steps))
	for i, s := range steps {
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
					Target:   o.Next,
					Loopback: idxOf[o.Next] <= i,
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
		out[i] = ds
	}
	return out
}
