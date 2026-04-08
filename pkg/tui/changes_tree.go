package tui

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"
	"github.com/kgruel/subtask/pkg/git"
)

type changesDir struct {
	name  string
	dirs  map[string]*changesDir
	files []git.DiffFileStat
}

func buildChangesTreeLines(files []git.DiffFileStat, selectedPath string, panelWidth int) ([]string, map[string]int, []string) {
	root := &changesDir{dirs: map[string]*changesDir{}}
	for _, f := range files {
		insertChangesFile(root, f)
	}
	collapseChangesDirs(root)

	var lines []string
	var orderedPaths []string
	index := make(map[string]int)
	renderChangesTree(root, nil, 0, selectedPath, panelWidth, &lines, index, &orderedPaths)
	return lines, index, orderedPaths
}

func insertChangesFile(root *changesDir, f git.DiffFileStat) {
	parts := strings.Split(f.Path, "/")
	if len(parts) == 1 {
		root.files = append(root.files, f)
		return
	}

	cur := root
	for _, p := range parts[:len(parts)-1] {
		if cur.dirs == nil {
			cur.dirs = map[string]*changesDir{}
		}
		next := cur.dirs[p]
		if next == nil {
			next = &changesDir{name: p, dirs: map[string]*changesDir{}}
			cur.dirs[p] = next
		}
		cur = next
	}
	cur.files = append(cur.files, f)
}

func collapseChangesDirs(d *changesDir) {
	for _, child := range d.dirs {
		collapseChangesDirs(child)
	}
	for {
		if len(d.files) != 0 || len(d.dirs) != 1 {
			return
		}
		var only *changesDir
		for _, v := range d.dirs {
			only = v
		}
		d.name = strings.TrimSuffix(d.name+"/"+only.name, "/")
		d.files = only.files
		d.dirs = only.dirs
	}
}

func renderChangesTree(d *changesDir, ancestorsHaveNext []bool, depth int, selectedPath string, panelWidth int, lines *[]string, index map[string]int, orderedPaths *[]string) {
	// Root has no label; render children directly.
	type entry struct {
		dir  *changesDir
		file *git.DiffFileStat
	}

	dirNames := make([]string, 0, len(d.dirs))
	for k := range d.dirs {
		dirNames = append(dirNames, k)
	}
	sort.Strings(dirNames)

	var entries []entry
	for _, name := range dirNames {
		entries = append(entries, entry{dir: d.dirs[name]})
	}

	sort.Slice(d.files, func(i, j int) bool { return d.files[i].Path < d.files[j].Path })
	for i := range d.files {
		entries = append(entries, entry{file: &d.files[i]})
	}

	isRoot := depth == 0
	for i, e := range entries {
		isLast := i == len(entries)-1
		var prefixPlain, prefix string
		if !isRoot {
			prefixPlain = changesTreePrefix(ancestorsHaveNext, isLast)
			prefix = styleChangesSep.Render(prefixPlain)
		}

		if e.dir != nil {
			label := "📁 " + e.dir.name
			label = ansi.Truncate(label, max(0, panelWidth-ansi.StringWidth(prefixPlain)), "")
			line := lipgloss.NewStyle().Width(panelWidth).Render(prefix + styleChangesTreeDir.Render(label))
			*lines = append(*lines, line)
			// Children of root dirs get tree lines with no leading indent
			var childAncestors []bool
			if isRoot {
				childAncestors = []bool{} // Root children: direct tree lines, no indent
			} else {
				childAncestors = append(ancestorsHaveNext, !isLast)
			}
			renderChangesTree(e.dir, childAncestors, depth+1, selectedPath, panelWidth, lines, index, orderedPaths)
			continue
		}

		f := *e.file
		icon := changesIcon(f)
		name := filepath.Base(f.Path)

		// Calculate available width for name, then pad line to full width
		usedWidth := ansi.StringWidth(prefixPlain) + ansi.StringWidth(icon) + 1 // +1 for space
		nameW := max(0, panelWidth-usedWidth)
		name = ansi.Truncate(name, nameW, "")

		// Build plain content, pad to full width
		plainContent := prefixPlain + icon + " " + name
		contentWidth := ansi.StringWidth(plainContent)
		if contentWidth < panelWidth {
			plainContent += strings.Repeat(" ", panelWidth-contentWidth)
		}

		// Apply styles based on selection state
		var line string
		if f.Path == selectedPath {
			// Selected: apply selection background to full line, then colorize parts
			line = styleChangesTreeSelected.Render(plainContent)
		} else {
			// Not selected: colorize prefix and icon/name separately
			iconRendered := lipgloss.NewStyle().Foreground(changesStatusColor(f)).Render(icon)
			nameRendered := lipgloss.NewStyle().Foreground(changesStatusColor(f)).Render(name)
			padding := ""
			if contentWidth < panelWidth {
				padding = strings.Repeat(" ", panelWidth-contentWidth)
			}
			line = prefix + iconRendered + " " + nameRendered + padding
		}
		line = zone.Mark(zoneDiffFile(f.Path), line)

		index[f.Path] = len(*lines)
		*lines = append(*lines, line)
		*orderedPaths = append(*orderedPaths, f.Path)
	}
}

func changesTreePrefix(ancestorsHaveNext []bool, isLast bool) string {
	var b strings.Builder
	for _, hasNext := range ancestorsHaveNext {
		if hasNext {
			b.WriteString("│   ")
		} else {
			b.WriteString("    ")
		}
	}
	if isLast {
		b.WriteString("└── ")
	} else {
		b.WriteString("├── ")
	}
	return b.String()
}

func changesIcon(s git.DiffFileStat) string {
	if s.Binary {
		return "■"
	}
	switch s.Status {
	case "A":
		return "+"
	case "D":
		return "⛌"
	default:
		return "●"
	}
}

func changesStatusColor(s git.DiffFileStat) lipgloss.TerminalColor {
	if s.Binary {
		return lipgloss.AdaptiveColor{Light: "242", Dark: "240"}
	}
	switch s.Status {
	case "A":
		return colorGreen
	case "D":
		return colorRed
	default:
		return colorYellow
	}
}
