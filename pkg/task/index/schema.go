package index

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const schemaVersion = 7

func migrateSchema(ctx context.Context, db *sql.DB) error {
	var v int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version;").Scan(&v); err != nil {
		return fmt.Errorf("read index schema version: %w", err)
	}
	if v == schemaVersion {
		return nil
	}
	if v > schemaVersion {
		return fmt.Errorf("index schema version %d is newer than supported %d", v, schemaVersion)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()

	if v == 0 {
		if err := migrateToV1(ctx, tx); err != nil {
			return err
		}
		v = 1
	}

	if v == 1 {
		if err := migrateToV2(ctx, tx); err != nil {
			return err
		}
		v = 2
	}

	if v == 2 {
		if err := migrateToV3(ctx, tx); err != nil {
			return err
		}
		v = 3
	}

	if v == 3 {
		if err := migrateToV4(ctx, tx); err != nil {
			return err
		}
		v = 4
	}

	if v == 4 {
		if err := migrateToV5(ctx, tx); err != nil {
			return err
		}
		v = 5
	}

	if v == 5 {
		if err := migrateToV6(ctx, tx); err != nil {
			return err
		}
		v = 6
	}

	if v == 6 {
		if err := migrateToV7(ctx, tx); err != nil {
			return err
		}
		v = 7
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d;", v)); err != nil {
		return fmt.Errorf("set index schema version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

func migrateToV1(ctx context.Context, tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS tasks (
			name TEXT PRIMARY KEY,

			title TEXT,
			base_branch TEXT,
			base_commit TEXT,
			follow_up TEXT,
			model TEXT,
			reasoning TEXT,
			description TEXT,

			state_status TEXT,
			effective_status TEXT,
			stage TEXT,
			workspace TEXT,
			started_at_ns INTEGER NOT NULL DEFAULT 0,
			merged INTEGER NOT NULL DEFAULT 0,
			supervisor_pid INTEGER NOT NULL DEFAULT 0,
			agent_replied INTEGER NOT NULL DEFAULT 0,
			last_error TEXT,

			tool_calls INTEGER NOT NULL DEFAULT 0,
			last_active_ns INTEGER NOT NULL DEFAULT 0,

			progress_done INTEGER NOT NULL DEFAULT 0,
			progress_total INTEGER NOT NULL DEFAULT 0,

			status_rank INTEGER NOT NULL DEFAULT 5,
			closed_at_ns INTEGER NOT NULL DEFAULT 0,

			files_sig TEXT NOT NULL DEFAULT '',

			git_lines_added INTEGER,
			git_lines_removed INTEGER,
			git_commits_behind INTEGER,
			git_conflict_files_json TEXT,
			git_integrated_reason TEXT,
			git_base_ref TEXT,
			git_target_ref TEXT,
			git_computed_at_ns INTEGER,
			git_error TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS tasks_sort_idx ON tasks(status_rank, last_active_ns DESC, name);`,
		`CREATE INDEX IF NOT EXISTS tasks_closed_idx ON tasks(closed_at_ns DESC, name);`,
		`CREATE INDEX IF NOT EXISTS tasks_status_idx ON tasks(effective_status);`,
	}

	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate v1: %w", err)
		}
	}
	return nil
}

func migrateToV2(ctx context.Context, tx *sql.Tx) error {
	stmts := []string{
		`DROP TABLE IF EXISTS tasks;`,
		`DROP INDEX IF EXISTS tasks_sort_idx;`,
		`DROP INDEX IF EXISTS tasks_closed_idx;`,
		`DROP INDEX IF EXISTS tasks_status_idx;`,
		`CREATE TABLE IF NOT EXISTS tasks (
			name TEXT PRIMARY KEY,

			title TEXT,
			base_branch TEXT,
			base_commit TEXT,
			follow_up TEXT,
			model TEXT,
			reasoning TEXT,
			description TEXT,

			task_schema INTEGER NOT NULL DEFAULT 0,
			task_status TEXT,
			worker_status TEXT,
			stage TEXT,
			workspace TEXT,
			started_at_ns INTEGER NOT NULL DEFAULT 0,
			supervisor_pid INTEGER NOT NULL DEFAULT 0,
			last_error TEXT,

			last_history_ns INTEGER NOT NULL DEFAULT 0,
			last_active_ns INTEGER NOT NULL DEFAULT 0,
			tool_calls INTEGER NOT NULL DEFAULT 0,

			last_run_duration_ms INTEGER NOT NULL DEFAULT 0,

			progress_done INTEGER NOT NULL DEFAULT 0,
			progress_total INTEGER NOT NULL DEFAULT 0,

			status_rank INTEGER NOT NULL DEFAULT 5,

			files_sig TEXT NOT NULL DEFAULT '',

			git_lines_added INTEGER,
			git_lines_removed INTEGER,
			git_commits_behind INTEGER,
			git_conflict_files_json TEXT,
			git_integrated_reason TEXT,
			git_base_ref TEXT,
			git_target_ref TEXT,
			git_computed_at_ns INTEGER,
			git_error TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS tasks_sort_idx ON tasks(status_rank, last_history_ns DESC, name);`,
		`CREATE INDEX IF NOT EXISTS tasks_status_idx ON tasks(task_status);`,
	}

	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate v2: %w", err)
		}
	}
	return nil
}

func migrateToV3(ctx context.Context, tx *sql.Tx) error {
	// Add base_commit (needed for behind/conflict checks) to tasks table.
	// Safe to run once; on fresh databases it will already exist from v2.
	if _, err := tx.ExecContext(ctx, `ALTER TABLE tasks ADD COLUMN base_commit TEXT;`); err != nil {
		// If it already exists, ignore.
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migrate v3: %w", err)
		}
	}
	return nil
}

func migrateToV4(ctx context.Context, tx *sql.Tx) error {
	// WorkerStatusNotStarted is represented as empty string. Prior versions used "idle".
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET worker_status = '' WHERE worker_status = 'idle';`); err != nil {
		return fmt.Errorf("migrate v4: %w", err)
	}
	return nil
}

func migrateToV5(ctx context.Context, tx *sql.Tx) error {
	// Integration tracking and git ref snapshot meta.
	stmts := []string{
		`ALTER TABLE tasks ADD COLUMN git_last_branch_head TEXT;`,
		`ALTER TABLE tasks ADD COLUMN git_patch_id TEXT;`,
		`ALTER TABLE tasks ADD COLUMN git_integrated_branch_head TEXT;`,
		`ALTER TABLE tasks ADD COLUMN git_integrated_target_head TEXT;`,
		`ALTER TABLE tasks ADD COLUMN git_integrated_checked_at_ns INTEGER;`,
		`CREATE TABLE IF NOT EXISTS index_meta (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			git_refs_snapshot_json TEXT,
			git_refs_snapshot_hash TEXT,
			git_refs_snapshot_at_ns INTEGER
		);`,
		`INSERT OR IGNORE INTO index_meta (id) VALUES (1);`,
	}

	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			// ALTER TABLE is not idempotent; ignore duplicate column errors.
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return fmt.Errorf("migrate v5: %w", err)
		}
	}
	return nil
}

func migrateToV6(ctx context.Context, tx *sql.Tx) error {
	// WorkerStatusRunning was renamed from "running" to "working".
	if _, err := tx.ExecContext(ctx, `UPDATE tasks SET worker_status = 'working' WHERE worker_status = 'running';`); err != nil {
		return fmt.Errorf("migrate v6: %w", err)
	}
	return nil
}

func migrateToV7(ctx context.Context, tx *sql.Tx) error {
	// Git redesign: store historical diffs + commit counts with input-based invalidation,
	// plus basic ref heads for debugging.
	stmts := []string{
		`ALTER TABLE tasks ADD COLUMN branch_head TEXT;`,
		`ALTER TABLE tasks ADD COLUMN base_head TEXT;`,

		`ALTER TABLE tasks ADD COLUMN changes_added INTEGER;`,
		`ALTER TABLE tasks ADD COLUMN changes_removed INTEGER;`,
		`ALTER TABLE tasks ADD COLUMN changes_base_commit TEXT;`,
		`ALTER TABLE tasks ADD COLUMN changes_branch_head TEXT;`,

		`ALTER TABLE tasks ADD COLUMN commit_count INTEGER;`,
		`ALTER TABLE tasks ADD COLUMN commit_count_base_commit TEXT;`,
		`ALTER TABLE tasks ADD COLUMN commit_count_branch_head TEXT;`,

		`ALTER TABLE tasks ADD COLUMN commit_log_last_head TEXT;`,
	}

	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			// ALTER TABLE is not idempotent; ignore duplicate column errors.
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return fmt.Errorf("migrate v7: %w", err)
		}
	}
	return nil
}
