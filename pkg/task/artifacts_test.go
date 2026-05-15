package task

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeHistoryLine(t *testing.T, path string, eventType string, data any) {
	t.Helper()
	raw, err := json.Marshal(data)
	require.NoError(t, err)
	line, err := json.Marshal(map[string]any{"type": eventType, "data": json.RawMessage(raw)})
	require.NoError(t, err)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	require.NoError(t, err)
}

func TestArtifacts_Empty(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	arts, err := Artifacts("test/empty")
	require.NoError(t, err)
	require.Empty(t, arts)
}

func TestArtifacts_SinglePresent(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	taskName := "test/arts"
	taskDir := Dir(taskName)
	require.NoError(t, os.MkdirAll(taskDir, 0o755))

	// Write the artifact file to disk.
	artifactPath := filepath.Join(taskDir, "reviews", "r1.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(artifactPath), 0o755))
	require.NoError(t, os.WriteFile(artifactPath, []byte("hello"), 0o644))

	histPath := HistoryPath(taskName)
	writeHistoryLine(t, histPath, "artifact.produced", map[string]string{
		"name": "r1.md",
		"path": "reviews/r1.md",
		"kind": "review",
	})

	arts, err := Artifacts(taskName)
	require.NoError(t, err)
	require.Len(t, arts, 1)
	require.Equal(t, "r1.md", arts[0].Name)
	require.Equal(t, "reviews/r1.md", arts[0].Path)
	require.Equal(t, "review", arts[0].Kind)
	require.Equal(t, int64(5), arts[0].Size)
	require.False(t, arts[0].Missing)
}

func TestArtifacts_MissingFile(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	taskName := "test/missing"
	taskDir := Dir(taskName)
	require.NoError(t, os.MkdirAll(taskDir, 0o755))

	histPath := HistoryPath(taskName)
	writeHistoryLine(t, histPath, "artifact.produced", map[string]string{
		"name": "gone.md",
		"path": "reviews/gone.md",
		"kind": "review",
	})

	arts, err := Artifacts(taskName)
	require.NoError(t, err)
	require.Len(t, arts, 1)
	require.True(t, arts[0].Missing)
	require.Equal(t, int64(0), arts[0].Size)
}

func TestArtifacts_DeduplicateLastWriteWins(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	taskName := "test/dedup"
	taskDir := Dir(taskName)
	require.NoError(t, os.MkdirAll(taskDir, 0o755))

	artifactPath := filepath.Join(taskDir, "reviews", "r1.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(artifactPath), 0o755))
	require.NoError(t, os.WriteFile(artifactPath, []byte("updated"), 0o644))

	histPath := HistoryPath(taskName)
	// First emission (will be superseded).
	writeHistoryLine(t, histPath, "artifact.produced", map[string]string{
		"name": "r1.md",
		"path": "reviews/r1.md",
		"kind": "old",
	})
	// Second emission on same path (last-write-wins).
	writeHistoryLine(t, histPath, "artifact.produced", map[string]string{
		"name": "r1.md",
		"path": "reviews/r1.md",
		"kind": "review",
	})

	arts, err := Artifacts(taskName)
	require.NoError(t, err)
	require.Len(t, arts, 1)
	require.Equal(t, "review", arts[0].Kind)
}

func TestArtifacts_LargeLineBeforeArtifact(t *testing.T) {
	// Regression: bufio.Scanner default 64KiB limit caused sc.Err()=ErrTooLong
	// when a large reply event preceded artifact.produced, silently dropping artifacts.
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	taskName := "test/large-line"
	taskDir := Dir(taskName)
	require.NoError(t, os.MkdirAll(taskDir, 0o755))

	histPath := HistoryPath(taskName)
	// Write a large message event (>64KiB) that would break the default scanner.
	bigContent := make([]byte, 128*1024)
	for i := range bigContent {
		bigContent[i] = 'x'
	}
	writeHistoryLine(t, histPath, "message", map[string]string{"content": string(bigContent)})
	// Artifact emitted after the large line.
	writeHistoryLine(t, histPath, "artifact.produced", map[string]string{
		"name": "out.md",
		"path": "reviews/out.md",
		"kind": "review",
	})

	arts, err := Artifacts(taskName)
	require.NoError(t, err)
	require.Len(t, arts, 1, "artifact must be found even when a >64KiB line precedes it")
	require.Equal(t, "out.md", arts[0].Name)
}

func TestArtifacts_EmissionOrder(t *testing.T) {
	origDir, _ := os.Getwd()
	tmpDir := t.TempDir()
	require.NoError(t, os.Chdir(tmpDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	taskName := "test/order"
	taskDir := Dir(taskName)
	require.NoError(t, os.MkdirAll(taskDir, 0o755))

	histPath := HistoryPath(taskName)
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		writeHistoryLine(t, histPath, "artifact.produced", map[string]string{
			"name": name,
			"path": "reviews/" + name,
			"kind": "review",
		})
	}

	arts, err := Artifacts(taskName)
	require.NoError(t, err)
	require.Len(t, arts, 3)
	require.Equal(t, "a.md", arts[0].Name)
	require.Equal(t, "b.md", arts[1].Name)
	require.Equal(t, "c.md", arts[2].Name)
}
