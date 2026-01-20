package diffparse

import (
	"strings"
	"testing"
)

func TestParseUnified_OmitsMetaAndHunkHeaders(t *testing.T) {
	in := strings.TrimSpace(`
diff --git a/a.txt b/a.txt
index 0000000..1111111 100644
--- a/a.txt
+++ b/a.txt
@@ -1,2 +1,2 @@
-hello
+hello world
 foo
`) + "\n"

	doc, err := ParseUnified(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseUnified: %v", err)
	}

	for _, r := range doc.Unified {
		if strings.Contains(r.Text, "diff --git") || strings.HasPrefix(r.Text, "@@") {
			t.Fatalf("unexpected meta/hunk in unified: %+v", r)
		}
	}
	if len(doc.Unified) == 0 || len(doc.SideBySide) == 0 {
		t.Fatalf("expected rows")
	}
}

func TestParseUnified_UnifiedRowsHavePrefixTypes(t *testing.T) {
	in := strings.TrimSpace(`
@@ -1,3 +1,3 @@
 a
-b
+c
 d
`) + "\n"

	doc, err := ParseUnified(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseUnified: %v", err)
	}

	var kinds []Kind
	for _, r := range doc.Unified {
		if r.Kind == KindSeparator {
			continue
		}
		kinds = append(kinds, r.Kind)
	}
	want := []Kind{KindContext, KindDelete, KindAdd, KindContext}
	if len(kinds) != len(want) {
		t.Fatalf("kinds: got=%v want=%v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds[%d]=%v want=%v", i, kinds[i], want[i])
		}
	}
}

func TestParseUnified_SideBySidePairsModify(t *testing.T) {
	in := strings.TrimSpace(`
@@ -1,2 +1,2 @@
-hello
+hello world
 foo
`) + "\n"

	doc, err := ParseUnified(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseUnified: %v", err)
	}

	foundModify := false
	for _, r := range doc.SideBySide {
		if r.Kind == KindModify {
			foundModify = true
			if r.OldText != "hello" || r.NewText != "hello world" || r.OldLine != 1 || r.NewLine != 1 {
				t.Fatalf("bad modify row: %+v", r)
			}
		}
	}
	if !foundModify {
		t.Fatalf("expected KindModify row")
	}
}

func TestParseUnified_EmptyAddedLinePreserved(t *testing.T) {
	in := strings.TrimSpace(`
@@ -0,0 +1,2 @@
+
+x
`) + "\n"

	doc, err := ParseUnified(strings.NewReader(in))
	if err != nil {
		t.Fatalf("ParseUnified: %v", err)
	}

	// First add line should have empty Text.
	found := false
	for _, r := range doc.Unified {
		if r.Kind == KindAdd && r.NewLine == 1 {
			found = true
			if r.Text != "" {
				t.Fatalf("expected empty text, got %q", r.Text)
			}
		}
	}
	if !found {
		t.Fatalf("expected empty add line")
	}
}

func TestParseHunkHeader(t *testing.T) {
	cases := []struct {
		in   string
		old  int
		new  int
		want bool
	}{
		{"@@ -1,2 +3,4 @@", 1, 3, true},
		{"@@ -10 +20 @@ foo", 10, 20, true},
		{"@@ -x +y @@", 0, 0, false},
		{"not a hunk", 0, 0, false},
	}

	for _, tc := range cases {
		old, nw, ok := parseHunkHeader(tc.in)
		if ok != tc.want {
			t.Fatalf("parseHunkHeader(%q) ok=%v want=%v", tc.in, ok, tc.want)
		}
		if !ok {
			continue
		}
		if old != tc.old || nw != tc.new {
			t.Fatalf("parseHunkHeader(%q)=%d,%d want=%d,%d", tc.in, old, nw, tc.old, tc.new)
		}
	}
}
