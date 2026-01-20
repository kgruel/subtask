package index

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
)

func isCorruptionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "database disk image is malformed"):
		return true
	case strings.Contains(msg, "file is not a database"):
		return true
	case strings.Contains(msg, "malformed database schema"):
		return true
	case strings.Contains(msg, "disk I/O error"):
		return true
	default:
		return false
	}
}

func (i *Index) rebuild(ctx context.Context) error {
	if i.db != nil {
		_ = i.db.Close()
		i.db = nil
	}

	ts := i.now().UTC().Format(time.RFC3339)
	ts = strings.NewReplacer(":", "", "-", "").Replace(ts)
	corruptPath := fmt.Sprintf("%s.corrupt-%s", i.path, ts)

	// Move existing files out of the way (best-effort).
	if err := renameIfExists(i.path, corruptPath); err != nil {
		return err
	}
	_ = renameIfExists(i.path+"-wal", corruptPath+"-wal")
	_ = renameIfExists(i.path+"-shm", corruptPath+"-shm")

	db, err := sql.Open("sqlite", i.path)
	if err != nil {
		return fmt.Errorf("rebuild index: open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	i.db = db

	if err := i.init(ctx); err != nil {
		_ = db.Close()
		i.db = nil
		return fmt.Errorf("rebuild index: %w", err)
	}
	return nil
}

func renameIfExists(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("move corrupt index %q -> %q: %w", src, dst, err)
	}
	return nil
}
