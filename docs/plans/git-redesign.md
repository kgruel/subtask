# Git Redesign: Historical Diffs + Ancestor Merge Detection

## 1. Overview / Goals

Subtask’s “task freshness” problems came from tying task status + change stats to moving git state (base branch advancing, branches being rewritten/deleted) and from having multiple ad-hoc readers spread across CLI/TUI/ops.

This redesign makes the task view:

- **Correct**: never show stale/wrong data; return explicit, typed errors when something is not computable.
- **Fast**: `list`/TUI should feel instant; recompute only for tasks whose inputs changed; parallelize recompute for 1–10 active tasks.
- **Simple**: one access layer (`pkg/task/store`) owns all caching + invalidation + locking.

### “Dream” UX

For non-git users:
- `subtask list` shows a small set of stable, meaningful fields: status + historical change stats (like GitHub PR “Files changed”), plus explicit error text when something is broken.
- Tasks show as merged automatically if Subtask can **prove** the task branch tip landed in base (ancestor check), without requiring the user to understand Git internals.

For git experts:
- `subtask show` provides detail inputs (base commit, branch head, base head), commit count, and commit timeline entries.
- Explicit errors mention the ref/commit that’s missing and how to fix it (fetch/restore branch/etc.).

## 2. Key Decisions

1) **Historical diff (not live)** (GitHub-style)
- `Changes` is a stable historical metric of “what the task contributed”.
- It does **not** rebase against a moving base head and does **not** collapse to `+0 -0` when base later contains the changes.

2) **Ancestor-only merge detection**
- Detect “merged” only when `branch_head` is an ancestor of the current base head.
- No “content detection” (no squash/cherry-pick detection). This avoids false positives and matches GitHub’s “indirect merge” behavior.

3) **Auto-write `task.merged` on detection**
- When ancestor detection triggers for an open task, Subtask appends a durable `task.merged` event (with frozen stats).
- After that, the task is durably merged; no continued re-detection is required.

4) **Closed stays closed**
- Closed tasks are immutable. They never auto-promote to merged later.

5) **Log commits to history (PR-style timeline)**
- When Subtask observes new commits on an open task branch, it appends `task.commit` events so the timeline reflects actual git activity.

6) **Show commit count (detail only)**
- Detail view shows `Commits: N` where `N` is commits on the task branch since `base_commit`.
- This replaces “commits behind base”; we do not show “behind”.

## 3. Data Model

### 3.1 `base_commit`: meaning + capture

`base_commit` is the immutable “starting point” for the task’s historical diff and commit count:

- **Definition**: the base branch HEAD at task creation time.
- **Capture time**: when the task is created/opened (e.g., `subtask draft`/`subtask send` that creates the branch).
- **Persistence**: stored durably (see below) so the task’s “Files changed” equivalent stays stable across base advances.

### 3.2 How `Changes` are computed

For open tasks (and for frozen merged/closed tasks, once recorded):

```
Changes = diffstat(base_commit..branch_head)
```

- Inputs: `base_commit`, `branch_head`
- Output: `added`, `removed` (optionally `files_changed`)
- Errors:
  - `ErrBranchMissing` / `ErrBranchDeleted`
  - `ErrBaseMissing` (if we cannot resolve `base_commit` due to repo corruption)
  - `ErrCommitMissing` (if `base_commit` or `branch_head` object no longer exists locally)

### 3.3 SQLite schema (columns needed)

SQLite remains a local cache/projection. It stores *cached derived values with their inputs* so the store can be fast and correct without TTL.

Minimum per-task columns (conceptual):

- Identity + file-backed:
  - `name TEXT PRIMARY KEY`
  - file sigs / updated timestamps (existing mechanism)

- Task git anchors:
  - `base_branch TEXT` (e.g. `main`)
  - `base_commit TEXT` (immutable once set)

- Heads (current inputs as last observed by Subtask):
  - `branch_head TEXT` (current `refs/heads/<task>` tip if exists)
  - `base_head TEXT` (current `refs/heads/<base_branch>` tip as observed; used for merge detection)

- Cached historical metrics (for open tasks):
  - `changes_added INTEGER`
  - `changes_removed INTEGER`
  - `changes_base_commit TEXT` (input)
  - `changes_branch_head TEXT` (input)
  - `commit_count INTEGER`
  - `commit_count_base_commit TEXT` (input)
  - `commit_count_branch_head TEXT` (input)

- Commit logging bookkeeping:
  - `commit_log_last_head TEXT` (last `branch_head` we scanned/logged)
  - optionally a lightweight dedupe aid:
    - `commit_log_seen_patch_ids_json TEXT` (bounded; for rebase/amend dedupe), OR keep SHA-only and accept duplicates.

This design intentionally avoids repo-wide snapshots, “commits behind”, and “integration reason” columns.

### 3.4 History event schemas

History (`history.jsonl`) is the portable source of truth.

#### `task.commit`

Appended when Subtask *observes* new commits on an open task branch.

```json
{
  "type": "task.commit",
  "sha": "abc123...",
  "subject": "Add tests for X",
  "author_name": "Codex",
  "author_email": "codex@example.com",
  "authored_at": 1730000000,
  "seen_at": 1730000100
}
```

Notes:
- We log what git says (author/subject/time). UI can render “worker committed …” based on author identity or simply “commit …”.
- Rebases/amends may produce new SHAs; see edge cases.

#### `task.merged` (via detected)

Appended when ancestor detection proves the task branch tip is in base.

```json
{
  "type": "task.merged",
  "via": "detected",
  "method": "ancestor",
  "base_branch": "main",
  "base_commit": "deadbeef...",
  "branch_head": "abc123...",
  "base_head": "fedcba...",
  "changes_added": 50,
  "changes_removed": 10,
  "commit_count": 5,
  "detected_at": 1730000200
}
```

Notes:
- This freezes the historical stats at the moment we mark merged.
- This is durable status (requirement: “merged only from history”).

## 4. Components

### 4.1 `pkg/task/store`

The single access layer for all readers (CLI, TUI, internal ops).

Responsibilities:
- Load file-backed task state (TASK.md/history/state/progress) with strict validation.
- Gather git inputs cheaply (refs for all tasks + base branches).
- Ensure returned views are correct for current inputs:
  - use cached values only when input-equal
  - recompute invalid tasks in parallel (bounded pool)
- Perform any required writes under lock:
  - append `task.commit` events
  - append `task.merged` event when detection triggers
  - update SQLite cache rows

API shape:

```go
type Store interface {
  List(ctx context.Context, opts ListOptions) (ListResult, error)
  Get(ctx context.Context, name string, opts GetOptions) (TaskView, error)
}
```

`ListResult` includes partial errors (`[]TaskLoadError`) and CLI must print them.

### 4.2 `pkg/task/index`

SQLite cache/projection.

Responsibilities:
- Store per-task cached derived values with their inputs.
- Store bookkeeping for commit logging scans.
- Keep transactions short; git work happens outside DB transactions.

### 4.3 `pkg/git`

Minimal primitives:
- `ListRefs(refs []string) map[string]sha` (uses `git for-each-ref`).
- `MergeBaseIsAncestor(ancestor, descendant) bool` (uses `git merge-base --is-ancestor`).
- `DiffStat(baseCommit, branchHead) (added, removed, filesChanged, error)`
- `RevListCount(baseCommit, branchHead) (int, error)`
- `ListCommits(baseCommit, branchHead) ([]CommitMeta, error)` for commit logging
- `CommitMeta(sha)` (subject/author/times) if needed independently

## 5. Flows

### 5.1 `subtask list` / TUI list

1) `store.List` enumerates tasks and validates task files.
2) Store gathers current refs (single git call).
3) For each task:
   - If task is durably merged/closed: read frozen stats from history (and optionally cache in SQLite for speed).
   - If open:
     - ensure `base_commit` is known (see migration).
     - compute or reuse cached `Changes` and (optionally) minimal fields needed for list.
4) Store returns `ListResult{Tasks, Errors}`; CLI prints errors.

### 5.2 `subtask show <task>`

1) `store.Get` loads the task and ensures all detail fields are correct for current inputs.
2) Computes:
   - `Changes` (historical diff)
   - `Commits: N` (rev-list count)
   - commit timeline entries (from history)
3) May append `task.commit` / `task.merged` if required (under lock) before returning.

### 5.3 When worker commits

No polling requirement. On any subsequent store access that observes `branch_head` changed:

1) Store lists commits between `commit_log_last_head..branch_head` (or `base_commit..branch_head` on first run).
2) Appends `task.commit` events for newly observed commits.
3) Updates `commit_log_last_head` in SQLite.

### 5.4 When external merge is detected (ancestor-only)

On store access for an open task:

1) Resolve `branch_head` and `base_head`.
2) If `merge-base --is-ancestor branch_head base_head`:
   - compute frozen stats (`diff(base_commit..branch_head)`, commit count)
   - append `task.merged` (`via=detected`)
   - free workspace if policy requires (optional; out of scope here)
3) Subsequent reads treat the task as durably merged (history).

### 5.5 `subtask merge` vs detected merge

- `subtask merge` continues to perform Subtask’s standard merge flow and appends `task.merged` with `via="subtask"` (and its merge metadata if applicable).
- Detected merge appends `task.merged` with `via="detected"` and `method="ancestor"`.
- Both produce the same durable status + frozen stats behavior for UX.

## 6. Edge Cases

- **Branch deleted**
  - Open task: `Changes.Err = ErrBranchDeleted` (if we know it existed) or `ErrBranchMissing`.
  - Detected merges cannot be proven without `branch_head` unless we have a cached commit SHA and it still exists locally.

- **Rebase/amend (commit logging)**
  - SHA-only logging: rebases create new SHAs; history shows both. Correct, but noisy.
  - Optional patch-id dedupe reduces noise without requiring complex “rewrite” events.

- **Git version compatibility**
  - Ancestor-only detection uses `git merge-base --is-ancestor` (very old; works on Git 2.34.x).
  - No `merge-tree --write-tree` usage in this design.

## 7. What We’re NOT Doing

- No content detection (no squash/cherry-pick “merged” detection).
- No “commits behind base”.
- No promotion of closed tasks to merged.

## 8. Migration

From current `dev`:

- Remove the existing complex “integration” subsystem and any UI display paths that can show “merged” without a `task.merged` event.
- Introduce `base_commit`:
  - New tasks record it at creation/open time (durably).
  - Existing tasks:
    - If `base_commit` is missing, initialize once on first access as the then-current base head (or merge-base if that is the closest available anchor), persist it, and treat it as the task’s historical anchor going forward.
    - Document that legacy tasks may not perfectly match “true original base” if created before this feature.
- Update list/show rendering to use historical `Changes` and detail commit count.
- Add/extend e2e tests:
  - external merge via merge commit / fast-forward (ancestor) triggers `task.merged via=detected`
  - commit logging appends `task.commit`
  - rebase/amend behavior (either allowed-noisy or patch-id dedupe)
