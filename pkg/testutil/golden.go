package testutil

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/zippoxer/subtask/pkg/render"
)

var updateGolden *bool

func init() {
	// Only define the flag if the test binary hasn't defined it already.
	if flag.Lookup("update") == nil {
		updateGolden = flag.Bool("update", false, "update golden files")
	}
}

func shouldUpdateGolden() bool {
	if updateGolden != nil {
		if *updateGolden {
			return true
		}
	} else if f := flag.Lookup("update"); f != nil {
		if v, err := strconv.ParseBool(f.Value.String()); err == nil && v {
			return true
		}
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SUBTASK_UPDATE_GOLDEN"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func assertGolden(t *testing.T, relPath string, got string, callerSkip int) {
	t.Helper()

	_, callerFile, _, ok := runtime.Caller(callerSkip)
	if !ok {
		t.Fatalf("failed to resolve golden path for %q", relPath)
	}

	relPath = filepath.FromSlash(relPath)
	path := filepath.Join(filepath.Dir(callerFile), relPath)
	got = normalizeNewlines(got)

	if shouldUpdateGolden() {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", path, err)
		}
		return
	}

	wantBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	want := normalizeNewlines(string(wantBytes))

	if got == want {
		return
	}

	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}

	contextStart := i - 80
	if contextStart < 0 {
		contextStart = 0
	}
	contextEndGot := i + 160
	if contextEndGot > len(got) {
		contextEndGot = len(got)
	}
	contextEndWant := i + 160
	if contextEndWant > len(want) {
		contextEndWant = len(want)
	}

	t.Fatalf(
		"golden mismatch: %s\n\nfirst diff at byte %d\n\n--- want (context)\n%s\n--- got (context)\n%s\n\nre-run with -update to accept new output",
		path,
		i,
		want[contextStart:contextEndWant],
		got[contextStart:contextEndGot],
	)
}

// AssertGolden compares got to the contents of relPath.
// If -update (or SUBTASK_UPDATE_GOLDEN) is set, it rewrites the golden file.
//
// relPath is resolved relative to the calling test file's directory (so it works
// even if the test changes the process cwd).
func AssertGolden(t *testing.T, relPath string, got string) {
	t.Helper()
	assertGolden(t, relPath, got, 2)
}

// AssertGoldenOutput compares got to a golden file based on the current output mode.
// In pretty mode it uses a `.ansi` file, otherwise it uses a `.txt` file.
//
// relBasePath is resolved relative to the calling test file's directory.
func AssertGoldenOutput(t *testing.T, relBasePath string, got string) {
	t.Helper()

	ext := ".txt"
	if render.Pretty {
		ext = ".ansi"
	}

	assertGolden(t, relBasePath+ext, got, 2)
}
