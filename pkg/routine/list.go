package routine

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// RoutineSummary is a lightweight description of a routine for listing purposes.
type RoutineSummary struct {
	Name          string   `json:"name"`
	Source        string   `json:"source"`
	Description   string   `json:"description"`
	EntryStep     string   `json:"entry_step"`
	TerminalSteps []string `json:"terminal_steps"`
}

// List returns all available routines: embedded canonicals plus any project
// overrides in .subtask/routines/*.yaml. When a project file shadows a
// canonical by name, it appears once with Source = SourceShadow. Results
// are sorted alphabetically by name.
func List() ([]RoutineSummary, error) {
	// Enumerate embedded canonical names.
	entries, err := embeddedTemplates.ReadDir("templates")
	if err != nil {
		return nil, fmt.Errorf("read embedded routine templates: %w", err)
	}
	canonicalNames := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		canonicalNames[strings.TrimSuffix(e.Name(), ".yaml")] = struct{}{}
	}

	// Enumerate project routine files (.subtask/routines/*.yaml).
	dir := RoutinesDir()
	projectEntries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	projectNames := make(map[string]struct{})
	for _, e := range projectEntries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
			projectNames[strings.TrimSuffix(e.Name(), ".yaml")] = struct{}{}
		}
	}

	// Build sorted union of all names.
	seen := make(map[string]struct{})
	var names []string
	for name := range canonicalNames {
		names = append(names, name)
		seen[name] = struct{}{}
	}
	for name := range projectNames {
		if _, already := seen[name]; !already {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	// Load each routine and build summary.
	summaries := make([]RoutineSummary, 0, len(names))
	for _, name := range names {
		r, err := LoadByName(name)
		if err != nil {
			return nil, fmt.Errorf("load routine %q: %w", name, err)
		}
		_, isCanonical := canonicalNames[name]
		_, isProject := projectNames[name]

		var source string
		switch {
		case isCanonical && isProject:
			source = SourceShadow
		case isCanonical:
			source = SourceCanonical
		default:
			source = SourceProject
		}

		var terminals []string
		for _, s := range r.Steps {
			if s.Kind == KindTerminal {
				terminals = append(terminals, s.ID)
			}
		}
		if terminals == nil {
			terminals = []string{}
		}

		summaries = append(summaries, RoutineSummary{
			Name:          name,
			Source:        source,
			Description:   r.Description,
			EntryStep:     r.EntryStep(),
			TerminalSteps: terminals,
		})
	}
	return summaries, nil
}
