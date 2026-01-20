# "Conflicts" shown for overlapping files, not actual git conflicts

## Summary

The TUI shows "⚠ X file(s) with conflicts" for files changed by both the task branch and the base branch, even when git would merge them cleanly. This is misleading - users expect "conflicts" to mean actual merge conflicts that need manual resolution.

## The Problem

Current behavior:
- `OverlappingFiles()` finds files changed in BOTH branches since the merge base
- TUI displays these as "conflicts" with a warning icon
- User sees "5 files with conflicts" and thinks merge will fail

Reality:
- These are just files touched by both sides
- Git can often merge them cleanly if changes are in different parts of the file
- Actual conflicts only surface when running `subtask merge` (during rebase)

## Example

```
Task branch changes: src/foo.go (lines 10-20)
Main branch changes: src/foo.go (lines 100-110)
```

TUI shows: "⚠ 1 file(s) with conflicts"
Actual merge result: Success (different parts of file)

## Code Location

- `git/git.go:228-253` - `OverlappingFiles()` function (heuristic, not real conflict detection)
- `tui/model_helpers.go:268` - Displays as "conflicts"
- `task/index/gitcache.go:187-206` - Populates `ConflictFiles` from overlapping files

## Options

### Option 1: Rename (quick fix)
Call it "Overlapping files" or "Files to review" instead of "Conflicts"
- Pro: Honest about what it is
- Con: Still not actionable information

### Option 2: Real conflict detection (recommended)
Use `git merge-tree --write-tree` to simulate the merge:
```bash
git merge-tree --write-tree main task-branch
```
This outputs actual conflict markers if the merge would fail.
- Pro: Shows real conflicts only
- Con: Slightly slower, requires git 2.38+

### Option 3: Remove entirely
Drop the "conflicts" indicator since it's misleading
- Pro: No false positives
- Con: Lose early warning about potential issues

## Impact

- Users delay merging because they think there are conflicts
- Creates anxiety about merge when none is warranted
- Erodes trust in subtask's conflict reporting

## Suggested Fix

Option 2 - Use `git merge-tree --write-tree` for accurate detection. Only show the Conflicts tab when there are actual merge conflicts, not just overlapping files.
