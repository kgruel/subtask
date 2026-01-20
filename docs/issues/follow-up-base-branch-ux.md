# `--follow-up` should imply using parent task's branch as base

## Summary

When using `--follow-up <task>` to create a task that builds on another task's work, the new task should default to using the parent task's branch as its base branch. Currently, `--follow-up` only carries conversation context but still requires explicit `--base-branch`, which leads to confusing situations.

## The Problem

The mental model when using `--follow-up`:
- "This new task builds on that task's changes"
- Expectation: new task is based on the parent task's branch

The reality:
- `--follow-up` only carries conversation context
- `--base-branch` defaults to main (or must be explicitly set)
- New task works against main, not the parent's changes

## Timeline (Real Incident)

1. **Task A: `web/design-alignment`** created
   - Restored UI to match old mock design
   - +1873 lines of changes
   - Status: Completed, waiting to merge

2. **Task B: `web/fix-foundation-bugs`** drafted as follow-up:
   ```bash
   subtask draft web/fix-foundation-bugs \
     --follow-up web/design-alignment \
     --base-branch main \
     --title "Fix foundational UI bugs"
   ```
   - Used `--follow-up web/design-alignment` (carries context)
   - But `--base-branch main` meant it was based on main, not design-alignment

3. **Task B ran for 43 minutes**
   - Worker tested UI with agent-browser, found and fixed bugs
   - But it was fixing bugs in the **old main UI**, not the restored UI from Task A
   - The fixes may not even apply to Task A's changes

4. **Task B merged into main**
   - Lead (me) didn't realize the mistake
   - Merged the "fixes" which were against the wrong codebase

5. **Task A still unmerged**
   - Now has conflicts with main (because Task B touched same files)
   - The actual UI restoration work was never tested or merged
   - The "follow-up" task's fixes may need to be redone

## Expected Behavior

When `--follow-up <task>` is used:

**Option A (Recommended):** Default `--base-branch` to the parent task's branch
```bash
# This should base on web/design-alignment branch, not main
subtask draft web/fix-foundation-bugs --follow-up web/design-alignment --title "..."
```

**Option B:** Warn if `--base-branch main` is used with `--follow-up` to an unmerged task
```
Warning: Task 'web/design-alignment' is not merged.
Using --base-branch main means this task won't include those changes.
Did you mean: --base-branch web/design-alignment ?
```

**Option C:** Require explicit acknowledgment
```bash
# Force user to think about it
subtask draft ... --follow-up web/design-alignment --base-branch main --ignore-unmerged
```

## Impact

- Wasted 43 minutes of worker time fixing bugs in the wrong codebase
- Merged changes that may conflict with the actual target
- Created confusion about what was actually fixed
- The original task (design-alignment) now has conflicts to resolve
- Follow-up fixes may need to be completely redone

## Suggested Fix

Make `--follow-up <task>` imply `--base-branch <task-branch>` by default when the parent task is unmerged. Allow `--base-branch main` to override if explicitly needed.
