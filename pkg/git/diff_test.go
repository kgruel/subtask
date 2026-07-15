package git

import (
	"os"
	"path/filepath"
	"testing"
)

// Fixture bytes below were captured empirically against live git (2.53.0) in
// a scratch repo — see the comments on parseNumstat/parseNameStatus in
// diff.go for the derivation. `git diff -z --numstat` emits
// "<added>\t<removed>\t<path>\x00" for plain entries, "-\t-\t<path>\x00" for
// binary, and "<added>\t<removed>\t\x00<old>\x00<new>\x00" for renames/copies
// (an empty path field, then two NUL-terminated paths). `git diff -z
// --name-status` emits "<code>\x00<path>\x00" for plain entries and
// "<code>\x00<old>\x00<new>\x00" for renames/copies.

func TestParseNumstatPlain(t *testing.T) {
	out := "1\t0\tnormal.txt\x00"
	stats := parseNumstat(out)
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d: %+v", len(stats), stats)
	}
	if stats[0].Path != "normal.txt" || stats[0].Added != 1 || stats[0].Removed != 0 || stats[0].Binary {
		t.Errorf("got %+v", stats[0])
	}
}

func TestParseNumstatRename(t *testing.T) {
	out := "1\t0\t\x00old.txt\x00new.txt\x00"
	stats := parseNumstat(out)
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d: %+v", len(stats), stats)
	}
	if stats[0].Path != "new.txt" || stats[0].Added != 1 || stats[0].Removed != 0 {
		t.Errorf("got %+v, want Path=new.txt +1/-0", stats[0])
	}
}

func TestParseNumstatCopy(t *testing.T) {
	out := "1\t0\t\x00sub/plain.txt\x00sub/plain2.txt\x00"
	stats := parseNumstat(out)
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d: %+v", len(stats), stats)
	}
	if stats[0].Path != "sub/plain2.txt" {
		t.Errorf("Path = %q, want sub/plain2.txt", stats[0].Path)
	}
}

func TestParseNumstatBinary(t *testing.T) {
	out := "-\t-\tbin.dat\x00"
	stats := parseNumstat(out)
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d: %+v", len(stats), stats)
	}
	if !stats[0].Binary || stats[0].Path != "bin.dat" {
		t.Errorf("got %+v, want Binary=true Path=bin.dat", stats[0])
	}
}

func TestParseNumstatFilenameContainsArrow(t *testing.T) {
	// A plain (non-rename) file whose literal name contains " => " must not
	// be mistaken for rename syntax now that parsing is structural.
	out := "1\t0\told => weird.txt\x00"
	stats := parseNumstat(out)
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d: %+v", len(stats), stats)
	}
	if stats[0].Path != "old => weird.txt" {
		t.Errorf("Path = %q, want %q", stats[0].Path, "old => weird.txt")
	}
}

func TestParseNumstatRenameFilenameContainsArrow(t *testing.T) {
	out := "1\t0\t\x00old => weird.txt\x00new => weird.txt\x00"
	stats := parseNumstat(out)
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d: %+v", len(stats), stats)
	}
	if stats[0].Path != "new => weird.txt" {
		t.Errorf("Path = %q, want %q", stats[0].Path, "new => weird.txt")
	}
}

func TestParseNumstatFilenameContainsBraces(t *testing.T) {
	out := "1\t0\twith{brace}.txt\x00"
	stats := parseNumstat(out)
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d: %+v", len(stats), stats)
	}
	if stats[0].Path != "with{brace}.txt" {
		t.Errorf("Path = %q, want %q", stats[0].Path, "with{brace}.txt")
	}
}

func TestParseNumstatFilenameContainsSpaces(t *testing.T) {
	out := "1\t0\twith spaces here.txt\x00"
	stats := parseNumstat(out)
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d: %+v", len(stats), stats)
	}
	if stats[0].Path != "with spaces here.txt" {
		t.Errorf("Path = %q, want %q", stats[0].Path, "with spaces here.txt")
	}
}

func TestParseNumstatFilenameContainsTab(t *testing.T) {
	if runtimeIsWindows() {
		t.Skip("tabs are not permitted in filenames on Windows")
	}
	// -z never C-quotes paths, so a literal tab in the filename passes
	// through untouched; parseNumstat must not split on it beyond the
	// leading "<added>\t<removed>\t" prefix.
	out := "1\t0\tfile\ttab.txt\x00"
	stats := parseNumstat(out)
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d: %+v", len(stats), stats)
	}
	if stats[0].Path != "file\ttab.txt" {
		t.Errorf("Path = %q, want %q", stats[0].Path, "file\ttab.txt")
	}
}

func TestParseNumstatMultipleRecords(t *testing.T) {
	out := "7\t0\t\x00old => weird.txt\x00new => weird.txt\x00" +
		"1\t0\t\x00sub/plain.txt\x00sub/plain2.txt\x00" +
		"-\t-\tbin.dat\x00" +
		"1\t0\tplain.txt\x00"
	stats := parseNumstat(out)
	want := []string{"new => weird.txt", "sub/plain2.txt", "bin.dat", "plain.txt"}
	if len(stats) != len(want) {
		t.Fatalf("expected %d stats, got %d: %+v", len(want), len(stats), stats)
	}
	for i, w := range want {
		if stats[i].Path != w {
			t.Errorf("stats[%d].Path = %q, want %q", i, stats[i].Path, w)
		}
	}
}

func TestParseNameStatusPlain(t *testing.T) {
	m := parseNameStatus("A\x00new.txt\x00M\x00existing.txt\x00")
	if m["new.txt"] != "A" || m["existing.txt"] != "M" {
		t.Errorf("got %+v", m)
	}
}

func TestParseNameStatusRename(t *testing.T) {
	m := parseNameStatus("R095\x00old.txt\x00new.txt\x00")
	if m["new.txt"] != "R" {
		t.Errorf("got %+v, want new.txt -> R", m)
	}
	if _, ok := m["old.txt"]; ok {
		t.Errorf("old path should not be keyed, got %+v", m)
	}
}

func TestParseNameStatusCopy(t *testing.T) {
	m := parseNameStatus("C050\x00sub/plain.txt\x00sub/plain2.txt\x00")
	if m["sub/plain2.txt"] != "C" {
		t.Errorf("got %+v, want sub/plain2.txt -> C", m)
	}
}

func TestParseNameStatusFilenameContainsArrowAndBraces(t *testing.T) {
	m := parseNameStatus("A\x00new => weird.txt\x00D\x00old => weird.txt\x00A\x00with{brace}.txt\x00")
	if m["new => weird.txt"] != "A" {
		t.Errorf("got %+v", m)
	}
	if m["old => weird.txt"] != "D" {
		t.Errorf("got %+v", m)
	}
	if m["with{brace}.txt"] != "A" {
		t.Errorf("got %+v", m)
	}
}

func TestParseNameStatusEmpty(t *testing.T) {
	m := parseNameStatus("")
	if len(m) != 0 {
		t.Errorf("expected empty map, got %+v", m)
	}
}

func runtimeIsWindows() bool {
	return os.PathSeparator == '\\'
}

// TestDiffNumstatRealRepoRenameWithArrow exercises the full DiffNumstat path
// (real `git diff -z --numstat` invocation, not a hand-built fixture) against
// a temp repo where a file containing the literal " => " substring is
// renamed to another name also containing " => ". Before this fix, the
// textual resolveRenamePath heuristic would have misparsed this.
func TestDiffNumstatRealRepoRenameWithArrow(t *testing.T) {
	// The point of this test is a real on-disk file whose name contains " => ",
	// the same token `git diff --numstat` uses to signal a rename — so parsing
	// must not be fooled by it. '>' is not a legal character in a Windows
	// filename, so the fixture cannot exist there and the scenario is
	// unreachable. The -z fixture-byte unit tests cover the parsing itself on
	// every platform.
	if runtimeIsWindows() {
		t.Skip(`filenames containing " => " are unrepresentable on Windows ('>' is invalid); -z fixture tests cover the parsing`)
	}

	dir := t.TempDir()
	runGit(t, dir, "init", "-q")
	runGit(t, dir, "config", "user.email", "a@b.com")
	runGit(t, dir, "config", "user.name", "test")

	oldName := "old => weird.txt"
	newName := "new => weird.txt"

	content := ""
	for range 50 {
		content += "line\n"
	}
	if err := os.WriteFile(filepath.Join(dir, oldName), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")

	if err := os.Rename(filepath.Join(dir, oldName), filepath.Join(dir, newName)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, newName), []byte(content+"extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "rename")

	stats, err := DiffNumstatRange(dir, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("DiffNumstatRange: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat, got %d: %+v", len(stats), stats)
	}
	if stats[0].Path != newName {
		t.Errorf("Path = %q, want %q", stats[0].Path, newName)
	}

	nameStatus, err := DiffNameStatusRange(dir, "HEAD~1", "HEAD")
	if err != nil {
		t.Fatalf("DiffNameStatusRange: %v", err)
	}
	if nameStatus[newName] != "R" {
		t.Errorf("name-status[%q] = %q, want R", newName, nameStatus[newName])
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := Output(dir, args...); err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
}
