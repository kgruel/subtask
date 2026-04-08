package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kgruel/subtask/internal/filelock"
	"github.com/kgruel/subtask/pkg/task"

	_ "modernc.org/sqlite"
)

const defaultBusyTimeout = 2 * time.Second

// Index is a SQLite-backed cache of task metadata for fast queries.
//
// The task files on disk remain the source of truth; the index can be rebuilt at any time.
type Index struct {
	db   *sql.DB
	path string
	now  func() time.Time
}

// OpenDefault opens (or creates) the index database at .subtask/index.db.
func OpenDefault() (*Index, error) {
	return Open(task.IndexPath())
}

// Open opens (or creates) the index database at path.
func Open(path string) (*Index, error) {
	if path == "" {
		return nil, errors.New("index db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create index dir: %w", err)
	}

	// Cross-process guardrail: avoid concurrent migrations/pragma races when multiple
	// `subtask` processes start at the same time (e.g. parallel `subtask list`).
	lockFile, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open index lock: %w", err)
	}
	if err := filelock.LockExclusive(lockFile); err != nil {
		_ = lockFile.Close()
		return nil, fmt.Errorf("lock index: %w", err)
	}
	defer func() {
		_ = filelock.Unlock(lockFile)
		_ = lockFile.Close()
	}()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	idx := &Index{db: db, path: path, now: time.Now}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := idx.init(ctx); err != nil {
		_ = db.Close()

		// If the database is corrupted (or not a database), rebuild it instead
		// of failing the command.
		if isCorruptionError(err) {
			if err2 := idx.rebuild(ctx); err2 != nil {
				return nil, err2
			}
		} else {
			return nil, err
		}
	}

	return idx, nil
}

func (i *Index) init(ctx context.Context) error {
	if err := pingWithRetry(ctx, i.db); err != nil {
		return fmt.Errorf("ping index db: %w", err)
	}

	// Pragmas: best-effort for speed + concurrency.
	if err := execPragmaWithRetry(ctx, i.db, "PRAGMA journal_mode=WAL;"); err != nil {
		return fmt.Errorf("pragma journal_mode: %w", err)
	}
	if err := execPragmaWithRetry(ctx, i.db, "PRAGMA synchronous=NORMAL;"); err != nil {
		return fmt.Errorf("pragma synchronous: %w", err)
	}
	if err := execPragmaWithRetry(ctx, i.db, fmt.Sprintf("PRAGMA busy_timeout=%d;", defaultBusyTimeout.Milliseconds())); err != nil {
		return fmt.Errorf("pragma busy_timeout: %w", err)
	}
	if err := execPragmaWithRetry(ctx, i.db, "PRAGMA foreign_keys=ON;"); err != nil {
		return fmt.Errorf("pragma foreign_keys: %w", err)
	}

	if err := migrateSchema(ctx, i.db); err != nil {
		return err
	}

	return nil
}

func pingWithRetry(ctx context.Context, db *sql.DB) error {
	for {
		err := db.PingContext(ctx)
		if err == nil {
			return nil
		}
		if !isSQLiteBusy(err) {
			return err
		}
		if ctx.Err() != nil {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func execPragmaWithRetry(ctx context.Context, db *sql.DB, query string) error {
	for {
		_, err := db.ExecContext(ctx, query)
		if err == nil {
			return nil
		}
		if !isSQLiteBusy(err) {
			return err
		}
		if ctx.Err() != nil {
			return err
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "SQLITE_BUSY") || strings.Contains(s, "database is locked")
}

// Close closes the underlying database connection.
func (i *Index) Close() error {
	if i == nil || i.db == nil {
		return nil
	}
	return i.db.Close()
}
