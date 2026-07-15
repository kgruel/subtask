// Package pathesc implements subtask's path-escaping convention: collapsing an
// absolute filesystem path into a single filename-safe directory component.
//
// It is the one definition of that convention, shared by every call site:
// workspace directory names (~/.subtask/workspaces/<escaped>--<id>), per-project
// runtime state (~/.subtask/projects/<escaped>/), and per-project log file names
// (.subtask/logs/<escaped>.log). Those names must agree — a workspace and the
// internal state describing it are looked up by the same escaped root — so the
// convention lives in exactly one place.
//
// It sits in internal/ rather than pkg/task because pkg/task imports pkg/logging,
// and both need to escape paths; a shared low-level package is the only way both
// can reach it without an import cycle.
package pathesc

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// MaxLen bounds the length of an escaped component.
//
// Escaping inlines a whole path into one filename, so a deep repo root produces
// a long component — and that component then sits inside another path. On
// Windows that nesting is fatal well before any single name limit: `git worktree
// add <path>` runs its child with GIT_DIR=<path>/.git, and git dies with
// "'$GIT_DIR' too big" once that exceeds PATH_MAX-40 == 220 (PATH_MAX is 260 in
// Git for Windows). Note core.longpaths cannot lift this: the check is a
// compile-time PATH_MAX comparison in git's setup.c that runs before config is
// read. Deep paths also break `git diff <sha>..<sha>`, which lstats the rev-range
// string against cwd to disambiguate revision-from-filename and dies with
// "Filename too long" rather than falling back.
//
// 64 keeps a workspace path near ~100 chars for a typical Windows home, leaving
// ample room under both limits, while still showing enough of the tail to
// identify the repo at a glance.
const MaxLen = 64

// hashLen is the hex width of the disambiguating prefix on a truncated
// component: 4 bytes of SHA-256. Truncation is lossy, so the hash — taken over
// the full pre-truncation string — is what keeps two distinct roots that share a
// tail (say, the same repo checked out under two parents) from colliding onto
// one workspace.
const hashLen = 8

// invalidChars are characters that are legal in a POSIX path but rejected by
// Windows filesystems. ':' covers the drive-letter colon, which is why an
// escaped Windows root reads "C--Users-me-repo": the drive letter is retained
// deliberately, since C:\repo and D:\repo are different roots.
var invalidChars = strings.NewReplacer(
	":", "-",
	"<", "-",
	">", "-",
	`"`, "-",
	"|", "-",
	"?", "-",
	"*", "-",
)

// Escape converts a path into a safe directory-name component.
//
// Symlinks are resolved first (best-effort, see Raw) so that different spellings
// of the same directory — /var vs /private/var on macOS, RUNNER~1 vs runneradmin
// 8.3 short names on Windows — escape identically. Without that, the same repo
// could be handed two different workspaces depending on how the caller spelled
// its cwd.
func Escape(p string) string {
	return truncate(Raw(p))
}

// Raw applies the escape convention without the length cap, reproducing the name
// scheme used before MaxLen existed. Callers that must still resolve directories
// created by an older subtask use it to look for a pre-cap name on disk; every
// new name comes from Escape.
func Raw(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		p = resolved
	}
	p = strings.ReplaceAll(p, string(os.PathSeparator), "-")
	return invalidChars.Replace(p)
}

// truncate caps an escaped component at MaxLen, keeping the tail and prefixing a
// hash of the full string. The tail is the informative end (the repo directory
// name lives there, not up in /Users/...), and the hash restores the uniqueness
// that truncation gives up.
func truncate(esc string) string {
	if len(esc) <= MaxLen {
		return esc
	}
	sum := sha256.Sum256([]byte(esc))
	h := hex.EncodeToString(sum[:hashLen/2])
	return h + "-" + esc[len(esc)-(MaxLen-hashLen-1):]
}
