package git

import (
	"fmt"
	"strings"
)

// ListRefs returns a map of full refname -> object SHA for the given ref namespaces/patterns.
//
// Example patterns:
// - "refs/heads"
// - "refs/remotes/origin"
//
// It runs a single `git for-each-ref` command and is intended to be fast.
func ListRefs(dir string, patterns ...string) (map[string]string, error) {
	args := []string{"for-each-ref", "--format=%(refname)%00%(objectname)"}
	args = append(args, patterns...)

	out, err := Output(dir, args...)
	if err != nil {
		return nil, fmt.Errorf("list refs: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return map[string]string{}, nil
	}

	m := make(map[string]string)
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x00", 2)
		if len(parts) != 2 {
			continue
		}
		ref := strings.TrimSpace(parts[0])
		sha := strings.TrimSpace(parts[1])
		if ref == "" || sha == "" {
			continue
		}
		m[ref] = sha
	}
	return m, nil
}
