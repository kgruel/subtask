package index

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type refsSnapshot struct {
	Hash string
	JSON string
	AtNS int64
}

func (i *Index) loadRefsSnapshot(ctx context.Context) (refsSnapshot, error) {
	var snap refsSnapshot
	var (
		hash sql.NullString
		js   sql.NullString
		at   sql.NullInt64
	)
	err := i.db.QueryRowContext(ctx, `SELECT git_refs_snapshot_hash, git_refs_snapshot_json, git_refs_snapshot_at_ns FROM index_meta WHERE id = 1;`).
		Scan(&hash, &js, &at)
	if err != nil {
		return refsSnapshot{}, fmt.Errorf("load index meta: %w", err)
	}
	if hash.Valid {
		snap.Hash = hash.String
	}
	if js.Valid {
		snap.JSON = js.String
	}
	if at.Valid {
		snap.AtNS = at.Int64
	}
	return snap, nil
}

func saveRefsSnapshot(ctx context.Context, tx *sql.Tx, hash, js string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
UPDATE index_meta SET
	git_refs_snapshot_hash = ?,
	git_refs_snapshot_json = ?,
	git_refs_snapshot_at_ns = ?
WHERE id = 1;`, nullableString(hash), nullableString(js), now.UnixNano())
	if err != nil {
		return fmt.Errorf("save index meta: %w", err)
	}
	return nil
}
