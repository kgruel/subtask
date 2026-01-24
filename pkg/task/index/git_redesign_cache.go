package index

import (
	"context"
	"fmt"
	"strings"
)

func (i *Index) UpdateRefHeads(ctx context.Context, name string, branchHead string, baseHead string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	branchHead = strings.TrimSpace(branchHead)
	baseHead = strings.TrimSpace(baseHead)

	_, err := i.db.ExecContext(ctx, `
UPDATE tasks
SET
	branch_head = ?,
	base_head = ?
WHERE name = ?;`,
		nullableString(branchHead),
		nullableString(baseHead),
		name,
	)
	if err != nil {
		return fmt.Errorf("index update ref heads: %w", err)
	}
	return nil
}

func (i *Index) UpdateChangesCache(ctx context.Context, name string, baseCommit string, branchHead string, added int, removed int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	baseCommit = strings.TrimSpace(baseCommit)
	branchHead = strings.TrimSpace(branchHead)

	_, err := i.db.ExecContext(ctx, `
UPDATE tasks
SET
	changes_added = ?,
	changes_removed = ?,
	changes_base_commit = ?,
	changes_branch_head = ?
WHERE name = ?;`,
		added,
		removed,
		nullableString(baseCommit),
		nullableString(branchHead),
		name,
	)
	if err != nil {
		return fmt.Errorf("index update changes cache: %w", err)
	}
	return nil
}

func (i *Index) UpdateCommitCountCache(ctx context.Context, name string, baseCommit string, branchHead string, count int) error {
	if ctx == nil {
		ctx = context.Background()
	}
	baseCommit = strings.TrimSpace(baseCommit)
	branchHead = strings.TrimSpace(branchHead)

	_, err := i.db.ExecContext(ctx, `
UPDATE tasks
SET
	commit_count = ?,
	commit_count_base_commit = ?,
	commit_count_branch_head = ?
WHERE name = ?;`,
		count,
		nullableString(baseCommit),
		nullableString(branchHead),
		name,
	)
	if err != nil {
		return fmt.Errorf("index update commit count cache: %w", err)
	}
	return nil
}

func (i *Index) UpdateCommitLogLastHead(ctx context.Context, name string, branchHead string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	branchHead = strings.TrimSpace(branchHead)

	_, err := i.db.ExecContext(ctx, `
UPDATE tasks
SET commit_log_last_head = ?
WHERE name = ?;`,
		nullableString(branchHead),
		name,
	)
	if err != nil {
		return fmt.Errorf("index update commit log last head: %w", err)
	}
	return nil
}

func (i *Index) UpdateIntegrationCache(ctx context.Context, name string, branchHead string, targetHead string, reason string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	branchHead = strings.TrimSpace(branchHead)
	targetHead = strings.TrimSpace(targetHead)
	reason = strings.TrimSpace(reason)

	_, err := i.db.ExecContext(ctx, `
UPDATE tasks
SET
	git_integrated_reason = ?,
	git_integrated_branch_head = ?,
	git_integrated_target_head = ?,
	git_integrated_checked_at_ns = ?
WHERE name = ?;`,
		nullableString(reason),
		nullableString(branchHead),
		nullableString(targetHead),
		i.now().UnixNano(),
		name,
	)
	if err != nil {
		return fmt.Errorf("index update integration cache: %w", err)
	}
	return nil
}
