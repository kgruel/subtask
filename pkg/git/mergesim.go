package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type mergeSimMethod string

const (
	mergeSimMethodMergeTree mergeSimMethod = "merge-tree"
	mergeSimMethodIndex     mergeSimMethod = "index"
)

const (
	mergeSimForceEnvVar = "SUBTASK_MERGE_SIM_FORCE" // "auto" (default), "merge-tree", or "index"
)

type mergeSimResult struct {
	Method        mergeSimMethod
	MergeBase     string
	MergedTree    string
	ConflictFiles []string
}

func simulateMerge(dir, targetRef, headRef string) (mergeSimResult, error) {
	targetRef = strings.TrimSpace(targetRef)
	headRef = strings.TrimSpace(headRef)
	if targetRef == "" || headRef == "" {
		return mergeSimResult{}, fmt.Errorf("targetRef and headRef are required")
	}

	mb, err := MergeBase(dir, targetRef, headRef)
	if err != nil {
		return mergeSimResult{}, err
	}
	mb = strings.TrimSpace(mb)
	if mb == "" {
		return mergeSimResult{}, fmt.Errorf("failed to resolve merge-base between %s and %s", targetRef, headRef)
	}

	method, err := selectMergeSimMethod()
	if err != nil {
		return mergeSimResult{}, err
	}

	switch method {
	case mergeSimMethodMergeTree:
		return simulateMergeMergeTree(dir, mb, targetRef, headRef)
	case mergeSimMethodIndex:
		return simulateMergeTempIndex(dir, mb, targetRef, headRef)
	default:
		return mergeSimResult{}, fmt.Errorf("unknown merge simulation method %q", method)
	}
}

func selectMergeSimMethod() (mergeSimMethod, error) {
	force := strings.ToLower(strings.TrimSpace(os.Getenv(mergeSimForceEnvVar)))
	switch force {
	case "", "auto":
		// auto
	case "merge-tree", "mergetree":
		if !mergeTreeWriteTreeSupported() {
			return "", fmt.Errorf("merge-tree simulation forced but git does not support merge-tree --write-tree")
		}
		return mergeSimMethodMergeTree, nil
	case "index", "temp-index", "tempindex":
		return mergeSimMethodIndex, nil
	default:
		// Unknown values fall back to auto (avoid breaking existing users).
	}

	if mergeTreeWriteTreeSupported() {
		return mergeSimMethodMergeTree, nil
	}
	return mergeSimMethodIndex, nil
}

var (
	mergeTreeWriteTreeOnce sync.Once
	mergeTreeWriteTreeOK   bool
)

func mergeTreeWriteTreeSupported() bool {
	mergeTreeWriteTreeOnce.Do(func() {
		cmd := exec.Command("git", "merge-tree", "-h")
		out, _ := cmd.CombinedOutput() // exit status is non-zero for -h
		s := string(out)

		// --write-tree is the key feature (introduced in git 2.38).
		// We also require --merge-base and --name-only since we rely on them.
		mergeTreeWriteTreeOK = strings.Contains(s, "--write-tree") && strings.Contains(s, "--merge-base") && strings.Contains(s, "--name-only")
	})
	return mergeTreeWriteTreeOK
}

func simulateMergeMergeTree(dir, mb, targetRef, headRef string) (mergeSimResult, error) {
	cmd := exec.Command("git", "merge-tree", "--write-tree", "--name-only", "--merge-base", mb, targetRef, headRef)
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()
	s := string(out)

	firstLine := ""
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		firstLine = strings.TrimSpace(s[:i])
	} else {
		firstLine = strings.TrimSpace(s)
	}

	if runErr == nil {
		if firstLine == "" {
			return mergeSimResult{}, fmt.Errorf("git merge-tree returned empty output")
		}
		return mergeSimResult{
			Method:     mergeSimMethodMergeTree,
			MergeBase:  mb,
			MergedTree: firstLine,
		}, nil
	}

	files := mergeTreeNameOnlyConflictFiles(s)
	if len(files) == 0 && strings.Contains(s, "CONFLICT") {
		files = extractMergeConflictFiles(s)
	}
	if len(files) == 0 {
		return mergeSimResult{}, fmt.Errorf("git merge-tree failed: %w", runErr)
	}
	return mergeSimResult{
		Method:        mergeSimMethodMergeTree,
		MergeBase:     mb,
		MergedTree:    firstLine,
		ConflictFiles: files,
	}, nil
}

const mergeSimTmpDirPrefix = "subtask-mergesim-"

var (
	mergeSimCleanupOnce sync.Once
)

func simulateMergeTempIndex(dir, mb, targetRef, headRef string) (mergeSimResult, error) {
	mergeSimCleanupOnce.Do(cleanupStaleMergeSimTmpDirs)

	tmpDir, err := os.MkdirTemp("", mergeSimTmpDirPrefix)
	if err != nil {
		return mergeSimResult{}, err
	}
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	// Important: do not pre-create the index file. If it exists but is empty,
	// git will error with "index file smaller than expected".
	idx := filepath.Join(tmpDir, "index")

	env := append(os.Environ(), "GIT_INDEX_FILE="+idx)

	if _, err := gitCombinedOutputWithEnv(dir, env, "read-tree", "-m", "-i", mb, targetRef, headRef); err != nil {
		return mergeSimResult{}, err
	}

	ls, err := gitCombinedOutputWithEnv(dir, env, "ls-files", "-u")
	if err != nil {
		return mergeSimResult{}, err
	}
	conflicts := parseUnmergedFiles(ls)
	if len(conflicts) > 0 {
		return mergeSimResult{
			Method:        mergeSimMethodIndex,
			MergeBase:     mb,
			ConflictFiles: conflicts,
		}, nil
	}

	tree, err := gitOutputWithEnv(dir, env, "write-tree")
	if err != nil {
		return mergeSimResult{}, err
	}
	tree = strings.TrimSpace(tree)
	if tree == "" {
		return mergeSimResult{}, fmt.Errorf("git write-tree returned empty output")
	}

	return mergeSimResult{
		Method:     mergeSimMethodIndex,
		MergeBase:  mb,
		MergedTree: tree,
	}, nil
}

func gitOutputWithEnv(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &Error{Dir: dir, Args: args, Stderr: stderr.String(), Cause: err}
	}
	return strings.TrimSpace(stdout.String()), nil
}

func gitCombinedOutputWithEnv(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", &Error{Dir: dir, Args: args, Stderr: string(out), Cause: err}
	}
	return string(out), nil
}

func parseUnmergedFiles(output string) []string {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	seen := make(map[string]struct{})
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "<mode> <oid> <stage>\t<path>"
		if _, file, ok := strings.Cut(line, "\t"); ok {
			file = strings.TrimSpace(file)
			if file != "" {
				seen[file] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	files := make([]string, 0, len(seen))
	for f := range seen {
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

func cleanupStaleMergeSimTmpDirs() {
	tmp := os.TempDir()
	entries, err := os.ReadDir(tmp)
	if err != nil {
		return
	}

	// Best-effort: remove stale temp dirs from crashed processes.
	// Keep the TTL comfortably above any single merge simulation duration to
	// avoid deleting dirs that are currently in use by other processes.
	const ttl = 10 * time.Minute
	cutoff := time.Now().Add(-ttl)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, mergeSimTmpDirPrefix) {
			continue
		}
		path := filepath.Join(tmp, name)
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(path)
	}
}
