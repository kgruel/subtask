package relpath

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestValidate_Rejects enumerates the path shapes that must never reach a
// caller, on every OS. The table is the ratchet: these are all forms that are
// absolute, escaping, or non-portable on at least one platform, and the point of
// the package is that the verdict does not depend on which platform runs it.
//
// "/etc/passwd" is the regression that motivated the shared validator: the old
// per-package checks used filepath.IsAbs, which is FALSE on Windows for a
// POSIX-absolute path, so it was accepted and silently re-anchored to
// <repo>\.subtask\etc\passwd.
func TestValidate_Rejects(t *testing.T) {
	for _, tc := range []struct{ p, why string }{
		{"/etc/passwd", "posix-absolute (filepath.IsAbs is false on Windows)"},
		{"/", "posix-absolute root"},
		{`C:\Windows\system32`, "windows-absolute with backslashes"},
		{"C:/Windows/system32", "drive-absolute with forward slashes"},
		{"C:x", "drive-relative"},
		{`\\server\share\x`, "UNC path"},
		{`notes\spec.md`, "backslash separator is not the authoring convention"},
		{"..", "traversal"},
		{"../outside.md", "traversal"},
		{"a/../../outside.md", "traversal escaping via .."},
		{"", "empty"},
		{"   ", "whitespace-only"},
	} {
		if err := Validate(tc.p, "prompt.file", ".subtask/"); err == nil {
			t.Errorf("Validate(%q) = nil, want error (%s)", tc.p, tc.why)
		}
	}
}

// TestValidate_Accepts pins the paths that must keep working — notably a nested
// forward-slash path, which a filepath.Clean-based check would rewrite to
// backslashes on Windows and then reject as non-portable.
func TestValidate_Accepts(t *testing.T) {
	for _, p := range []string{
		"prompt.md",
		"prompts/review.md",
		"prompts/nested/deep/review.md",
		"./prompt.md",
		"a/../prompt.md",
	} {
		if err := Validate(p, "prompt.file", ".subtask/"); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", p, err)
		}
	}
}

// TestResolveUnder_ContainsResult checks the resolved path is absolute and
// actually inside the anchor.
func TestResolveUnder_ContainsResult(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "repo", ".subtask")

	got, err := ResolveUnder(base, "prompts/review.md", "prompt.file", ".subtask/")
	if err != nil {
		t.Fatalf("ResolveUnder: %v", err)
	}
	want := filepath.Join(base, "prompts", "review.md")
	if got != want {
		t.Errorf("ResolveUnder = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, base) {
		t.Errorf("ResolveUnder = %q, escaped base %q", got, base)
	}
}

// TestResolveUnder_RejectsEscape is the end-to-end version of the bypass: a
// rejected path must produce no resolved location at all.
func TestResolveUnder_RejectsEscape(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "repo", ".subtask")

	for _, p := range []string{"/etc/passwd", "../../etc/passwd"} {
		got, err := ResolveUnder(base, p, "prompt.file", ".subtask/")
		if err == nil {
			t.Errorf("ResolveUnder(%q) = %q, want error", p, got)
		}
		if !strings.Contains(err.Error(), "prompt.file") {
			t.Errorf("ResolveUnder(%q) error %q should name the field", p, err)
		}
	}
}
