package git

import "testing"

func TestParseNumstatResolvesRenamePaths(t *testing.T) {
	out := "1\t0\tnormal.txt\n" +
		"3\t2\ta.txt => b.txt\n" +
		"4\t1\tdir/{a => b}.txt\n" +
		"5\t0\tpre/{old => new}/post.txt\n"

	stats := parseNumstat(out)
	if len(stats) != 4 {
		t.Fatalf("expected 4 stats, got %d: %+v", len(stats), stats)
	}

	want := []string{
		"normal.txt",
		"b.txt",
		"dir/b.txt",
		"pre/new/post.txt",
	}
	for i, w := range want {
		if stats[i].Path != w {
			t.Errorf("stats[%d].Path = %q, want %q", i, stats[i].Path, w)
		}
	}

	// Sanity-check the line counts survive the path rewrite.
	if stats[0].Added != 1 || stats[0].Removed != 0 {
		t.Errorf("normal.txt counts = +%d/-%d, want +1/-0", stats[0].Added, stats[0].Removed)
	}
	if stats[1].Added != 3 || stats[1].Removed != 2 {
		t.Errorf("b.txt counts = +%d/-%d, want +3/-2", stats[1].Added, stats[1].Removed)
	}
}

// TestParseNumstatRealGitRenameForms pins the three rename path shapes that
// `git diff --numstat` actually emits (verified against live git): a
// common-prefix brace, a common-prefix+suffix brace, and a no-common-segment
// plain arrow. These are the forms production hits, distinct from the
// hand-written fixtures above.
func TestParseNumstatRealGitRenameForms(t *testing.T) {
	out := "2\t1\tsrc/{old.txt => new.txt}\n" + // common prefix only
		"0\t0\t{src => a/x}/new.txt\n" + // common suffix only
		"0\t0\ta/x/new.txt => b/final.go\n" // no common segment
	stats := parseNumstat(out)

	want := []string{"src/new.txt", "a/x/new.txt", "b/final.go"}
	if len(stats) != len(want) {
		t.Fatalf("expected %d stats, got %d: %+v", len(want), len(stats), stats)
	}
	for i, w := range want {
		if stats[i].Path != w {
			t.Errorf("stats[%d].Path = %q, want %q", i, stats[i].Path, w)
		}
	}
}

func TestResolveRenamePathDegenerateBrace(t *testing.T) {
	// "{old => new}" with no surrounding path resolves to just the new path.
	if got := resolveRenamePath("{old => new}"); got != "new" {
		t.Errorf("resolveRenamePath(%q) = %q, want %q", "{old => new}", got, "new")
	}
	// A rename into a sibling dir.
	if got := resolveRenamePath("{src => dst}/file.go"); got != "dst/file.go" {
		t.Errorf("resolveRenamePath(%q) = %q, want %q", "{src => dst}/file.go", got, "dst/file.go")
	}
}
