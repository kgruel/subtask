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

func parseNumstat(out string) []DiffFileStat {
	if out == "" {
		return nil
	}

	var stats []DiffFileStat
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}

		s := DiffFileStat{Path: parts[2]}
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
// Output format:
// - <status>\t<path>
// - Renames/copies: R<score>\t<old>\t<new> (we return status "R" for <new>)
func DiffNameStatus(dir, baseRef string) (map[string]string, error) {
	out, err := Output(dir, "diff", "--name-status", baseRef)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

// DiffNameStatusRange returns per-file status codes for base..branch.
func DiffNameStatusRange(dir, baseRef, branchRef string) (map[string]string, error) {
	out, err := Output(dir, "diff", "--name-status", baseRef+".."+branchRef)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

func parseNameStatus(out string) map[string]string {
	if out == "" {
		return map[string]string{}
	}
	m := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		st := parts[0]
		if st == "" {
			continue
		}
		code := st[:1]
		switch code {
		case "R", "C":
			if len(parts) >= 3 {
				m[parts[2]] = code
			}
		default:
			m[parts[1]] = code
		}
	}
	return m
}

// DiffNumstat returns per-file diff stats compared to baseRef.
// Includes both committed changes on the current branch and uncommitted changes.
func DiffNumstat(dir, baseRef string) ([]DiffFileStat, error) {
	// git diff --numstat <baseRef>
	// Output format: <added>\t<removed>\t<file>
	// Binary files show "-" for both counts.
	out, err := Output(dir, "diff", "--numstat", baseRef)
	if err != nil {
		return nil, err
	}
	return parseNumstat(out), nil
}

// DiffNumstatRange returns per-file diff stats for base..branch.
func DiffNumstatRange(dir, baseRef, branchRef string) ([]DiffFileStat, error) {
	out, err := Output(dir, "diff", "--numstat", baseRef+".."+branchRef)
	if err != nil {
		return nil, err
	}
	return parseNumstat(out), nil
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
