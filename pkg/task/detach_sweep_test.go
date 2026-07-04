package task

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSweepOrphanedDetachPrompts covers the opportunistic cleanup of orphaned
// detached-supervisor prompt files: a live claim protects the file (the child
// may still read it), a fresh file is always kept (its parent may still be
// mid-handshake), and only a stale (past-threshold) file with no live claim is
// removed. File age is set deterministically with os.Chtimes relative to the
// package's detachPromptMaxAge, so the test never sleeps and never depends on
// the constant's exact value.
func TestSweepOrphanedDetachPrompts(t *testing.T) {
	t.Setenv("SUBTASK_DIR", t.TempDir())
	root := t.TempDir()
	initGitRepo(t, root)

	orig, _ := os.Getwd()
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	resetProjectCache()

	old := time.Now().Add(-2 * detachPromptMaxAge) // comfortably past the threshold
	fresh := time.Now()

	cases := []struct {
		name      string
		liveClaim bool
		mtime     time.Time
		wantKept  bool
	}{
		{name: "live claim keeps an old prompt", liveClaim: true, mtime: old, wantKept: true},
		{name: "no claim keeps a fresh prompt", liveClaim: false, mtime: fresh, wantKept: true},
		{name: "no claim removes an old prompt", liveClaim: false, mtime: old, wantKept: false},
	}

	names := make([]string, len(cases))
	files := make([]string, len(cases))
	for i, tc := range cases {
		taskName := fmt.Sprintf("sweep/case-%d", i)
		names[i] = taskName

		st := &State{}
		if tc.liveClaim {
			// This test process is alive, so the claim is not stale.
			st.SupervisorPID = os.Getpid()
		}
		require.NoError(t, st.Save(taskName))

		dir := DetachDir(taskName)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		pf := filepath.Join(dir, "detach-prompt-x.txt")
		require.NoError(t, os.WriteFile(pf, []byte("prompt body"), 0o644))
		require.NoError(t, os.Chtimes(pf, tc.mtime, tc.mtime))
		files[i] = pf
	}

	sweepOrphanedDetachPrompts(names)

	for i, tc := range cases {
		_, err := os.Stat(files[i])
		if tc.wantKept {
			require.NoError(t, err, "%s: prompt file should be kept", tc.name)
		} else {
			require.True(t, os.IsNotExist(err), "%s: prompt file should be swept", tc.name)
		}
	}
}
