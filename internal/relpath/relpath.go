// Package relpath validates the relative paths that subtask's YAML files point
// at: agent prompt.file, routine default_prompt.file, and routine
// produces:/consumes: artifacts.
//
// All of them share one trust boundary. A YAML file can arrive from a synced
// .subtask/ folder or a shared routines repo, so a path in it is untrusted input
// and must not be able to name a file outside its anchor. They also share one
// authoring convention: paths are written with forward slashes regardless of the
// OS running subtask. One definition, several callers — it lives here rather
// than in pkg/routine because pkg/agent cannot import pkg/routine (pkg/routine
// already imports pkg/agent).
package relpath

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// Validate checks that p is a portable, anchor-relative slash path.
//
// field names the YAML field in the error ("prompt.file"); anchor names what the
// path is relative to (".subtask/").
//
// Validation uses the slash-only "path" package rather than "filepath". On
// Windows filepath.Clean would rewrite "notes/spec.md" to "notes\spec.md" and
// then trip the backslash guard below, rejecting a perfectly valid nested path.
// Backslashes in the ORIGINAL input are rejected first, since that is about YAML
// authoring (forward slashes only), not the local OS's separator.
//
// This is pure path-shape validation and deliberately does not touch the
// filesystem: routine artifact paths are validated at load time, when the task
// folder they will be joined against does not exist yet.
func Validate(p, field, anchor string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("%s: empty or whitespace-only path", field)
	}
	if strings.Contains(p, `\`) {
		return fmt.Errorf("%s %q must use forward slashes only", field, p)
	}
	// filepath.IsAbs is not enough on its own: on Windows it is FALSE for
	// "/etc/passwd" (a Windows absolute path needs a drive letter or UNC
	// prefix), so a POSIX-absolute path would slip through and then be quietly
	// re-anchored inside the target folder. path.IsAbs catches that case on
	// every OS; filepath.IsAbs additionally catches UNC paths on Windows.
	if path.IsAbs(p) || filepath.IsAbs(p) {
		return fmt.Errorf("%s %q must be relative to %s, not absolute", field, p, anchor)
	}
	// Conversely, a drive-absolute path like "C:/tmp/x.md" or "C:x" is neither
	// path.IsAbs nor filepath.IsAbs on a Unix host — accepted on Unix, absolute
	// on Windows. A colon in the first segment is never valid in a portable
	// relative path, so reject it outright rather than relying on the host OS to
	// recognize its own drive syntax.
	firstSeg, _, _ := strings.Cut(p, "/")
	if strings.Contains(firstSeg, ":") {
		return fmt.Errorf("%s %q must be relative to %s, not absolute", field, p, anchor)
	}
	cleaned := path.Clean(p)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return fmt.Errorf("%s %q must stay inside %s (no `..` traversal)", field, p, anchor)
	}
	return nil
}

// ResolveUnder validates p and returns its absolute location under base.
//
// Returning an absolute, contained path means callers can read it without
// re-validating. Symlinks are deliberately not resolved: if a user drops a
// symlink inside .subtask/prompts/ pointing outside, that is a trust boundary
// they crossed by writing into their own repo. The check defends against
// malicious YAML reaching files the YAML author could not otherwise name.
func ResolveUnder(base, p, field, anchor string) (string, error) {
	if err := Validate(p, field, anchor); err != nil {
		return "", err
	}
	abs := filepath.Clean(filepath.Join(base, filepath.FromSlash(path.Clean(p))))

	// Validate already guarantees containment, so this is defence in depth
	// rather than the primary check — a caller passing a relative base, or a
	// future edit loosening Validate, should fail closed rather than silently
	// hand back a path outside the anchor.
	relBack, err := filepath.Rel(base, abs)
	if err != nil || relBack == ".." || strings.HasPrefix(relBack, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s %q must stay inside %s (no `..` traversal)", field, p, anchor)
	}
	return abs, nil
}
