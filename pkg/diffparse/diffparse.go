package diffparse

import (
	"bufio"
	"io"
	"regexp"
	"strconv"
	"strings"
)

type Kind uint8

const (
	KindSeparator Kind = iota
	KindContext
	KindAdd
	KindDelete
	KindModify // side-by-side only (paired delete+add)
)

type UnifiedRow struct {
	Kind    Kind
	OldLine int
	NewLine int
	Text    string
}

type SideBySideRow struct {
	Kind    Kind
	OldLine int
	NewLine int
	OldText string
	NewText string
}

type Document struct {
	Unified    []UnifiedRow
	SideBySide []SideBySideRow
	OldMaxLine int
	NewMaxLine int
	UnifiedOld int
	UnifiedNew int
}

var hunkHeaderRE = regexp.MustCompile(`^@@\s+-(\d+)(?:,(\d+))?\s+\+(\d+)(?:,(\d+))?\s+@@`)

// ParseUnified parses a unified diff (for a single file) into:
// - Unified rows (default rendering)
// - Side-by-side rows (optional toggle)
//
// The parser intentionally omits diff metadata and hunk headers so the UI matches diffnav's
// content-first style.
func ParseUnified(r io.Reader) (*Document, error) {
	p := &parser{doc: &Document{}}

	scanner := bufio.NewScanner(r)
	// Support very long lines (minified files, etc.).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		p.consume(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	p.flushSidePending()
	return p.doc, nil
}

type pendingLine struct {
	line int
	text string
}

type parser struct {
	doc *Document

	inHunk   bool
	seenHunk bool
	oldLine  int
	newLine  int

	sideDel []pendingLine
	sideAdd []pendingLine
}

func (p *parser) addUnified(r UnifiedRow) {
	if r.OldLine > p.doc.UnifiedOld {
		p.doc.UnifiedOld = r.OldLine
	}
	if r.NewLine > p.doc.UnifiedNew {
		p.doc.UnifiedNew = r.NewLine
	}
	p.doc.Unified = append(p.doc.Unified, r)
}

func (p *parser) addSide(r SideBySideRow) {
	if r.OldLine > p.doc.OldMaxLine {
		p.doc.OldMaxLine = r.OldLine
	}
	if r.NewLine > p.doc.NewMaxLine {
		p.doc.NewMaxLine = r.NewLine
	}
	p.doc.SideBySide = append(p.doc.SideBySide, r)
}

func (p *parser) addSeparator() {
	p.addUnified(UnifiedRow{Kind: KindSeparator})
	p.addSide(SideBySideRow{Kind: KindSeparator})
}

func (p *parser) consume(line string) {
	if oldStart, newStart, ok := parseHunkHeader(line); ok {
		p.flushSidePending()
		if p.seenHunk {
			p.addSeparator()
		}
		p.seenHunk = true
		p.inHunk = true
		p.oldLine = oldStart
		p.newLine = newStart
		return
	}

	// Hide diff metadata and non-hunk lines.
	if !p.inHunk {
		return
	}

	// Hide the "no newline" marker as delta renders it unobtrusively.
	if line == `\ No newline at end of file` {
		p.flushSidePending()
		return
	}

	if line == "" {
		// Defensive: treat as context line.
		p.flushSidePending()
		p.addUnified(UnifiedRow{Kind: KindContext, OldLine: p.oldLine, NewLine: p.newLine})
		p.addSide(SideBySideRow{Kind: KindContext, OldLine: p.oldLine, NewLine: p.newLine})
		p.oldLine++
		p.newLine++
		return
	}

	switch {
	case strings.HasPrefix(line, " "):
		p.flushSidePending()
		text := line[1:]
		p.addUnified(UnifiedRow{Kind: KindContext, OldLine: p.oldLine, NewLine: p.newLine, Text: text})
		p.addSide(SideBySideRow{Kind: KindContext, OldLine: p.oldLine, NewLine: p.newLine, OldText: text, NewText: text})
		p.oldLine++
		p.newLine++
	case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		text := line[1:]
		p.addUnified(UnifiedRow{Kind: KindDelete, OldLine: p.oldLine, NewLine: 0, Text: text})
		p.sideDel = append(p.sideDel, pendingLine{line: p.oldLine, text: text})
		p.oldLine++
	case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
		text := line[1:]
		p.addUnified(UnifiedRow{Kind: KindAdd, OldLine: 0, NewLine: p.newLine, Text: text})
		p.sideAdd = append(p.sideAdd, pendingLine{line: p.newLine, text: text})
		p.newLine++
	default:
		// Ignore any other lines (including header-ish lines that appear mid-stream).
		p.flushSidePending()
	}
}

func (p *parser) flushSidePending() {
	if len(p.sideDel) == 0 && len(p.sideAdd) == 0 {
		return
	}

	n := len(p.sideDel)
	if len(p.sideAdd) > n {
		n = len(p.sideAdd)
	}

	for i := 0; i < n; i++ {
		var (
			hasDel  bool
			hasAdd  bool
			oldLine int
			newLine int
			oldText string
			newText string
		)
		if i < len(p.sideDel) {
			hasDel = true
			oldLine = p.sideDel[i].line
			oldText = p.sideDel[i].text
		}
		if i < len(p.sideAdd) {
			hasAdd = true
			newLine = p.sideAdd[i].line
			newText = p.sideAdd[i].text
		}

		kind := KindContext
		switch {
		case hasDel && hasAdd:
			kind = KindModify
		case hasDel:
			kind = KindDelete
		case hasAdd:
			kind = KindAdd
		}

		p.addSide(SideBySideRow{
			Kind:    kind,
			OldLine: oldLine,
			NewLine: newLine,
			OldText: oldText,
			NewText: newText,
		})
	}

	p.sideDel = p.sideDel[:0]
	p.sideAdd = p.sideAdd[:0]
}

func parseHunkHeader(line string) (oldStart, newStart int, ok bool) {
	m := hunkHeaderRE.FindStringSubmatch(line)
	if m == nil {
		return 0, 0, false
	}
	oldStart, err1 := strconv.Atoi(m[1])
	newStart, err2 := strconv.Atoi(m[3])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return oldStart, newStart, true
}
