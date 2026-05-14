package index

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"

	"github.com/kgruel/subtask/pkg/task"
	"github.com/kgruel/subtask/pkg/testutil"
)

func TestMigrateToV8_FollowUpIndex(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	dbPath := task.IndexPath()

	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	// Build a minimal v7 schema (only what we need for the migration).
	_, err = db.Exec(`
CREATE TABLE tasks (
  name TEXT PRIMARY KEY,
  follow_up TEXT
);
PRAGMA user_version=7;
`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	idx, err := Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })

	// Confirm the index was created.
	var indexName string
	err = idx.db.QueryRowContext(context.Background(),
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_tasks_follow_up';`,
	).Scan(&indexName)
	require.NoError(t, err)
	require.Equal(t, "idx_tasks_follow_up", indexName)
}

func TestMigrateToV8_Idempotent(t *testing.T) {
	_ = testutil.NewTestEnv(t, 0)
	dbPath := task.IndexPath()

	require.NoError(t, os.MkdirAll(filepath.Dir(dbPath), 0o755))
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)

	_, err = db.Exec(`
CREATE TABLE tasks (
  name TEXT PRIMARY KEY,
  follow_up TEXT
);
PRAGMA user_version=7;
`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// First migration.
	idx, err := Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, idx.Close())

	// Manually reset version to 7 and run migration again.
	db2, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db2.Exec(`PRAGMA user_version=7;`)
	require.NoError(t, err)
	require.NoError(t, db2.Close())

	// Second migration — must not error.
	idx2, err := Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, idx2.Close())
}
