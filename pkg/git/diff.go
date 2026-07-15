package git

import (
	"fmt"
	"strconv"
	"strings"
)

type DiffFileStat struct {
	Path    string
	Added   int
	Removed int
	Binary  bool
	Status  string // from `git diff --name-status` (e.g. A/M/D/R/C/T/U)
}

// parseNumstat parses the NUL-delimited output of `git diff -z --numstat`.
//
// Verified against live git (2.53.0): each record is "<added>\t<removed>\t"
// followed by either a path terminated by NUL (plain add/modify/delete), or
// an empty field terminated by NUL followed by the old path (NUL-terminated)
// and the new path (NUL-terminated) for renames/copies. Because paths are
// never C-quoted under -z, this holds even for filenames containing tabs,
// braces, or the literal " => " sequence.
func parseNumstat(out string) []DiffFileStat {
	if out == "" {
		return nil
	}

	tokens := strings.Split(out, "\x00")
	// Split leaves a trailing empty token after the final NUL.
	if len(tokens) > 0 && tokens[len(tokens)-1] == "" {
		tokens = tokens[:len(tokens)-1]
	}

	var stats []DiffFileStat
	for i := 0; i < len(tokens); i++ {
		parts := strings.SplitN(tokens[i], "\t", 3)
		if len(parts) < 3 {
			continue
		}

		var path string
		if parts[2] != "" {
			path = parts[2]
		} else {
			// Rename/copy: old path, then new path, follow as separate tokens.
			i++
			if i+1 >= len(tokens) {
				break
			}
			path = tokens[i+1]
			i++
		}

		s := DiffFileStat{Path: path}
		if parts[0] == "-" || parts[1] == "-" {
			s.Binary = true
			stats = append(stats, s)
			continue
		}

		added, err1 := strconv.Atoi(parts[0])
		removed, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			continue
		}

		s.Added = added
		s.Removed = removed
		stats = append(stats, s)
	}

	return stats
}

// DiffNameStatus returns per-file status codes compared to baseRef.
// Includes both committed changes on the current branch and uncommitted changes.
//
// Output format (see parseNameStatus): NUL-delimited status/path records.
func DiffNameStatus(dir, baseRef string) (map[string]string, error) {
	out, err := Output(dir, "diff", "-z", "--name-status", baseRef)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

// DiffNameStatusRange returns per-file status codes for base..branch.
func DiffNameStatusRange(dir, baseRef, branchRef string) (map[string]string, error) {
	out, err := Output(dir, "diff", "-z", "--name-status", baseRef+".."+branchRef)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

// parseNameStatus parses the NUL-delimited output of `git diff -z --name-status`.
//
// Verified against live git (2.53.0): each record is a status code (e.g. "A",
// "M", "D", "R095", "C050") terminated by NUL, followed by one NUL-terminated
// path (add/modify/delete), or two NUL-terminated paths — old, then new — for
// renames/copies. We key renames/copies by the new path, matching parseNumstat.
func parseNameStatus(out string) map[string]string {
	if out == "" {
		return map[string]string{}
	}

	tokens := strings.Split(out, "\x00")
	if len(tokens) > 0 && tokens[len(tokens)-1] == "" {
		tokens = tokens[:len(tokens)-1]
	}

	m := make(map[string]string)
	for i := 0; i < len(tokens); i++ {
		st := tokens[i]
		if st == "" {
			continue
		}
		code := st[:1]
		switch code {
		case "R", "C":
			// old path, then new path.
			if i+2 >= len(tokens) {
				return m
			}
			m[tokens[i+2]] = code
			i += 2
		default:
			if i+1 >= len(tokens) {
				return m
			}
			m[tokens[i+1]] = code
			i++
		}
	}
	return m
}

// DiffNumstat returns per-file diff stats compared to baseRef.
// Includes both committed changes on the current branch and uncommitted changes.
func DiffNumstat(dir, baseRef string) ([]DiffFileStat, error) {
	// git diff -z --numstat <baseRef>, parsed structurally by parseNumstat.
	out, err := Output(dir, "diff", "-z", "--numstat", baseRef)
	if err != nil {
		return nil, err
	}
	return parseNumstat(out), nil
}

// DiffNumstatRange returns per-file diff stats for base..branch.
func DiffNumstatRange(dir, baseRef, branchRef string) ([]DiffFileStat, error) {
	out, err := Output(dir, "diff", "-z", "--numstat", baseRef+".."+branchRef)
	if err != nil {
		return nil, err
	}
	return parseNumstat(out), nil
}

// DiffStatRange returns summed added/removed lines for base..branch.
//
// This is committed-history only (does not include uncommitted workspace changes).
func DiffStatRange(dir, baseRef, branchRef string) (added, removed int, err error) {
	stats, err := DiffNumstatRange(dir, baseRef, branchRef)
	if err != nil {
		return 0, 0, err
	}
	for _, s := range stats {
		if s.Binary {
			continue
		}
		added += s.Added
		removed += s.Removed
	}
	return added, removed, nil
}

// DiffFile returns the unified diff for a single file path compared to baseRef.
func DiffFile(dir, baseRef, path string) (string, error) {
	return Output(dir, "diff", baseRef, "--", path)
}

// DiffFileRange returns the unified diff for a single file path for base..branch.
func DiffFileRange(dir, baseRef, branchRef, path string) (string, error) {
	return Output(dir, "diff", baseRef+".."+branchRef, "--", path)
}

// ResolveDiffBase returns the base commit to diff against.
//
// Always compute merge-base between branchRef (e.g. "HEAD" or the task branch)
// and the local baseBranch.
func ResolveDiffBase(dir string, branchRef, baseBranch string) (string, error) {
	if baseBranch == "" {
		return "", fmt.Errorf("cannot diff: task has no base branch")
	}

	target := baseBranch
	base, err := MergeBase(dir, branchRef, target)
	if err != nil {
		return "", fmt.Errorf("failed to compute merge-base between %s and %s: %w", branchRef, target, err)
	}
	if base == "" {
		return "", fmt.Errorf("failed to compute merge-base between %s and %s", branchRef, target)
	}
	return base, nil
}
