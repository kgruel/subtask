package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/task/history"
)

// TestEnsureSchema_ConcurrentWriterNotTruncated exercises the data-loss race
// that the WithLock wrapping fixes: a legacy (Schema<1) task with no history
// yet, migrated by EnsureSchema while a live writer appends events under the
// task lock. Before the fix EnsureSchema did an unlocked stat -> WriteAll, so
// it could O_TRUNC-rewrite history.jsonl after the writer had appended events,
// silently dropping them. The invariant that holds regardless of who wins the
// race is: every writer event must survive.
func TestEnsureSchema_ConcurrentWriterNotTruncated(t *testing.T) {
	// Self-contained setup: a temp cwd with a hand-written Schema-0 TASK.md.
	// No git init needed — with no git root, HistoryPath and the lock path both
	// fall back consistently under cwd/.subtask. buildSchema1History only shells
	// out to git for legacy merged/closed state, which this fixture lacks.
	t.Setenv("SUBTASK_DIR", t.TempDir())
	chdir(t, t.TempDir())

	const (
		iterations    = 50
		writerEvents  = 20
		writerRunBase = "writer"
	)

	for i := 0; i < iterations; i++ {
		taskName := fmt.Sprintf("legacy/race-%d", i)
		seedLegacyTask(t, taskName)

		// Start barrier so both goroutines hit the short stat->rewrite window.
		var start sync.WaitGroup
		start.Add(1)
		var done sync.WaitGroup
		done.Add(2)

		var migErr, writeErr error

		go func() {
			defer done.Done()
			start.Wait()
			migErr = EnsureSchema(taskName)
		}()

		go func() {
			defer done.Done()
			start.Wait()
			for j := 0; j < writerEvents; j++ {
				data, _ := json.Marshal(map[string]any{"seq": j})
				ev := history.Event{
					TS:      time.Now().UTC(),
					Type:    "message",
					Role:    "user",
					Content: fmt.Sprintf("%s-%d", writerRunBase, j),
					Data:    data,
				}
				// Append takes the task lock (the legitimate write path); a raw
				// unlocked AppendLocked would itself be a data race -race flags.
				if err := history.Append(taskName, ev); err != nil {
					writeErr = err
					return
				}
			}
		}()

		start.Done()
		done.Wait()

		require.NoError(t, migErr, "iteration %d: EnsureSchema", i)
		require.NoError(t, writeErr, "iteration %d: writer", i)

		// Every writer event must be present, regardless of interleaving.
		events, err := history.Read(taskName, history.ReadOptions{})
		require.NoError(t, err, "iteration %d: read history", i)

		seen := make(map[string]bool, writerEvents)
		for _, ev := range events {
			if ev.Type == "message" {
				seen[ev.Content] = true
			}
		}
		for j := 0; j < writerEvents; j++ {
			want := fmt.Sprintf("%s-%d", writerRunBase, j)
			require.Truef(t, seen[want], "iteration %d: writer event %q was truncated away", i, want)
		}
	}
}

// seedLegacyTask writes a minimal Schema-0 TASK.md (no history.jsonl) so
// EnsureSchema treats it as a legacy task needing migration.
func seedLegacyTask(t *testing.T, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(task.Dir(name), 0o755))
	content := "---\n" +
		"title: legacy race task\n" +
		"base-branch: main\n" +
		"---\n\n" +
		"legacy task body\n"
	require.NoError(t, os.WriteFile(task.Path(name), []byte(content), 0o644))

	loaded, err := task.Load(name)
	require.NoError(t, err)
	require.Less(t, loaded.Schema, CurrentSchema, "fixture must start below current schema")
	_, statErr := os.Stat(task.HistoryPath(name))
	require.True(t, os.IsNotExist(statErr), "fixture must start with no history.jsonl")
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
	// Resolve symlinks so canonicalPath() inside task paths matches.
	resolved, err := filepath.EvalSymlinks(dir)
	if err == nil && resolved != dir {
		require.NoError(t, os.Chdir(resolved))
	}
}
