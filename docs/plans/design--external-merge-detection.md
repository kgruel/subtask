# External merge detection (simplified, reliable)

## Problem

Subtask’s durable task status (`open|closed|merged`) is derived from `history.jsonl` events. Today, tasks become `merged` only when `subtask merge` appends a `task.merged` event.

If the task branch is integrated into the base branch externally (manual `git merge`, GitHub PR, etc.), Subtask still reports the task as `open` or `closed`, even though the branch’s changes are already in the base branch.

## Goals

- Detect that a task branch’s *net* changes are already present in the base branch (“integrated”), even if the merge happened outside Subtask.
- Avoid long SQLite locks (do git work outside DB transactions; keep writes small).
- Recompute only when inputs change (branch/base moved).
- Prefer *guarantees* over heuristics; keep the implementation small.

## Non-goals (first iteration)

- Perfect detection after the local task branch is deleted (possible but expensive; needs patch-id/commit search heuristics).
- Identifying *which* commit(s) on the base branch correspond to the task for squash merges (no single canonical commit).
- Network calls / GitHub API integration.

## GitHub reality check (what “merged” means there)

GitHub does **not** infer “merged” purely from git history; it stores PR state as metadata:

- A PR is “merged” when GitHub performed a merge action and recorded it (e.g., REST `GET /repos/{owner}/{repo}/pulls/{pull_number}` exposes `merged_at`, `merged`, and `merge_commit_sha`).
- GitHub *can* recognize **indirect merges** (commits become reachable on the base branch) and show a PR as merged in some cases, but this relies on commit ancestry (it won’t detect “same patch via squash” as a merge of that PR).

Implication for Subtask:
- If we want “reliable” without an API, we should lean on git primitives that provide guarantees about **reachability** and/or **no-op merges**, not try to mimic GitHub’s PR metadata.

References:
- GitHub REST API: “Get a pull request” and “Check if a pull request has been merged”: https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request and https://docs.github.com/en/rest/pulls/pulls#check-if-a-pull-request-has-been-merged
- GitHub docs: “Indirect merges” in “About pull request merges”: https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/incorporating-changes-from-a-pull-request/about-pull-request-merges#indirect-merges

## Current implementation (relevant code)

- Durable status: `task/history/history.go` (`Tail()` walks `task.opened|task.closed|task.merged`).
- Merge flow: `task/ops/merge.go` appends `task.merged` with a squash commit SHA and frees workspace.
- Git “integration” check exists already:
  - `git/git.go:IsIntegrated(dir, branch, target) IntegrationReason` (currently a multi-step ladder).
  - Cached in SQLite as `git_integrated_reason` via `task/index/gitcache.go`.
- `EffectiveTarget()` already prefers `origin/<base>` when it’s ahead to catch PR merges before the user pulls.

This design recommends simplifying integration detection down to two primitives (reachability + no-op merge) and updating the index cache logic accordingly.

## What does “integrated” mean?

For Subtask’s purposes:

> A task is “integrated” if merging the task branch into the base branch would produce **no tree changes** (i.e., a no-op merge).

This matches user intent (“the task’s changes are already in main”), and it’s robust across merge strategies.

## Git primitives: what’s guaranteed vs “best effort”

To keep this reliable and simple, use **two** git checks only:

1. **Reachability (guarantee for history-preserving merges)**  
   `git merge-base --is-ancestor <branchHead> <targetHead>`  
   If true, the exact branch tip commit is in the target’s history (fast-forward or merge commit). This is the same core primitive GitHub relies on for “indirect merges”.

2. **No-op merge (guarantee for content integration)**  
   Compute the tree that would result from merging, and compare it to the target tree:  
   `git merge-tree --write-tree <targetHead> <branchHead>` vs `git rev-parse <targetHead>^{tree}`  
   If equal (and no conflicts), merging would introduce no content changes. This covers squash merges, cherry-picks, and “applied elsewhere” cases without guesswork about commit identities.

Cost:
- Reachability is very cheap.
- `merge-tree --write-tree` is the expensive step, but we only do it when reachability is false *and* inputs changed.

## Proposed approach (simplified)

### What we show to users

Define a *computed* status (from the index) that can be shown as `✓ merged`:

- If durable history says `merged` → show merged.
- Else if git check says **integrated** → show merged (with reason, e.g. `ancestor` or `merge_adds_nothing`).
- Else show the durable status (`open`/`closed`).

This achieves “shows as merged” without silently mutating durable history.

### Durable status stays explicit

For **open** tasks, don’t auto-append `task.merged` on detection. This avoids surprising side effects (e.g., `subtask diff` expecting a merge commit SHA).

For **closed** tasks, once integration is proven, it’s reasonable to promote `closed → merged` by appending a `task.merged` event. This matches user intent (“it ended up merged”) and avoids the confusing state where a task is permanently “closed (not merged)” even though its content is in `main`.

- Summary:
  - Open tasks: integration affects display only (`✓ merged`), no durable mutation.
  - Closed tasks: integration proof appends `task.merged` (durable).

## Caching & invalidation under frequent base advances

The base branch advances frequently (subtask merges + user commits). Benchmarks show that re-evaluating `merge-tree` for *every* task on every base advance (or on every `list`) is too slow at 50–100 tasks.

So the design shifts to:

- Keep `list` fast by default (no per-task integration recompute).
- Do O(1) work after `send` (only one task changes).
- Do O(1) work after `merge` (only that task is merged by definition).
- Detect *external* git ref changes reliably and, when they occur, run an explicit “repair” pass before displaying output (can be slower, but only when refs changed outside Subtask).

### What we cache

**Per task (SQLite `tasks` table):**

- `git_last_branch_head TEXT`: last known commit SHA for the task branch (even if the branch ref is later deleted).
- `git_patch_id TEXT`: patch-id for the task diff vs its recorded `base_commit` (used for debugging/optional optimizations).
- `git_integrated_reason TEXT`: empty/NULL = unknown or not integrated, non-empty enum when integration is proven (`ancestor` or `merge_tree_noop`).
- `git_integrated_branch_head TEXT`: branch head used to prove integration.
- `git_integrated_target_head TEXT`: target head used to prove integration.
- `git_integrated_checked_at_ns INTEGER`: timestamp.

**Repository meta (new `index_meta` table, single-row):**

- `git_refs_snapshot_json TEXT`: JSON map of relevant refs to SHAs.
- `git_refs_snapshot_hash TEXT`: hash of the snapshot (fast compare).
- `git_refs_snapshot_at_ns INTEGER`: timestamp.

The snapshot covers:
- `refs/heads/<taskname>` for known tasks (if it exists).
- `refs/heads/<baseBranch>` and `refs/remotes/origin/<baseBranch>` for base branches used by tasks.

### How we detect “cache might be wrong”

On every `list`/TUI refresh (and `show`), compute a current refs snapshot using **one git command**:

- `git for-each-ref --format=%(refname)%00%(objectname) <ref...>`

If the hash matches `git_refs_snapshot_hash`, we know refs have not changed since Subtask last updated the index → cached integration state is valid.

If it differs, some refs changed outside Subtask (external merge, manual rebase, etc.) → run the repair pass before displaying output (so we don’t show stale state).

### Effective target choice

Use `git.EffectiveTarget(repoDir, baseBranch)` (prefer `origin/<base>` when it’s ahead *if it exists locally*). Note: Subtask can’t detect GitHub merges until the local repo has fetched updated refs.

## When we compute/recompute integration

### Normal path (no external git ref changes)

- After a worker finishes (`send` completes and the branch head changes): recompute integration **for that one task only**, update `git_last_branch_head`, and update the refs snapshot.
- After `subtask merge`: task is already known merged (durable `task.merged` event); update refs snapshot. No integration scan needed.
- `list`/TUI: read cached data; only pay the ~single-command refs snapshot check.

### Repair path (refs changed outside Subtask)

When `list`/`show` detects a snapshot mismatch:

1. Update the snapshot in the index meta row.
2. Recompute integration for tasks that are not already durable-merged:
   - First try reachability: `git merge-base --is-ancestor <branchHead> <targetHead>`.
   - If false, try no-op merge: `git merge-tree --write-tree <targetHead> <branchHead>` and compare to `<targetHead>^{tree}`.
3. Write updated `git_integrated_reason` (and heads) back to SQLite, then render.

This can take noticeable time at 50–100 tasks, but it only happens when git state changed outside Subtask (the case where correctness matters and some waiting is acceptable).

## Explicit durable transition (not implemented)

Subtask does not automatically convert “detected as integrated” into a durable `task.merged` event. This keeps Subtask from silently mutating history based on git heuristics.

## Edge cases

- **GitHub PR state**: without API integration, we won’t match GitHub’s “merged” metadata; we’re detecting repository integration.
- **Branch deleted**: reachability and no-op merge can still be checked if we cached `git_last_branch_head` and the commit object still exists locally.
- **Squash merges**: detected via the no-op merge check.

## Benchmarks (real numbers)

Environment:
- Apple M4 Pro, macOS kernel `25.1.0`, `git version 2.51.0`.
- Synthetic repos in `/tmp` with disjoint task file sets (no merge conflicts), 50 commits of base churn, and task branches with 2 commits touching 10 files each.

### Ref snapshot cost (100 task branches)

- `git for-each-ref refs/heads/task/`: median ~5.3ms (p95 ~6.9ms)
- `git show-ref --heads`: median ~5.2ms

This is cheap enough to run on every `list`/TUI refresh to detect external git changes.

### Integration checks cost

Single task (1 branch):
- `merge-base --is-ancestor`: median ~4.2ms
- `merge-tree --write-tree` + tree compare: median ~9.0ms
- Combined: median ~12.9ms

Batch (N tasks), measured as “loop N times spawning git each time” (same cost model as Subtask’s helpers):
- N=10: `is-ancestor` ~70ms; `merge-tree` ~82–84ms
- N=50: `is-ancestor` ~300–430ms; `merge-tree` ~330–460ms
- N=100: `is-ancestor` ~540–985ms; `merge-tree` ~635–1031ms

Conclusion: doing `merge-tree` (or even `is-ancestor`) for *all tasks* on every base advance or on every `list` does not meet the “never wait on list” requirement at 50–100 tasks. This is why the design uses a fast ref snapshot check and only runs a full repair pass when refs changed outside Subtask.

## Rough implementation plan

1. **Schema**: bump `task/index/schema.go` to v5.
   - Add per-task columns listed above (`git_last_branch_head`, `git_integrated_*`, optionally `git_patch_id`).
   - Add `index_meta` table for `git_refs_snapshot_*`.
2. **Ref snapshot**: implement snapshot build/compare using `git for-each-ref` and a stable hash.
3. **Mutation hooks**:
   - On `send` completion: update `git_last_branch_head` for the task; update refs snapshot.
   - On `merge`: update refs snapshot (and durable status already handled by history).
4. **Repair pass**:
   - Triggered only when snapshot mismatch is detected on `list`/`show`.
   - Runs integration checks for tasks not durable-merged, writes results to SQLite, then renders output.
5. **Presentation**: list/TUI/show display “merged” when either durable status is `merged` or `git_integrated_reason` is non-empty.
6. **Tests**: e2e coverage for:
   - history-preserving merge (ancestor) detection,
   - squash merge detection (merge-tree),
   - No manual command required; detection affects display only.
