# Task Freshness: Requirements

## Goals (what we're optimizing for)

1. **Never show wrong data** - prefer waiting over lying
2. **Feel instant** - list/show/detail should be fast
3. **Keep complexity in one place** - single unified access layer

## Assumptions (normal operation, not edge cases)

1. **1-10 tasks are actively worked on at any time** - workers commit regularly, this is normal
2. **Workers do arbitrary git operations** - commit, rebase, reset, amend, cherry-pick, etc.
3. **Local-first** - subtask doesn't run `git fetch`, only user actions change remote refs

## Requirements

1. **Universal correctness guarantee** - every access to task data gets correct data or an explicit error, never stale/wrong/placeholder values. This applies to:
   - TUI (list, detail)
   - CLI (`subtask list`, `subtask show`, etc.)
   - Internal code (any package that reads task data)

2. **Single access layer** - all readers go through `pkg/task/store`, which enforces the correctness guarantee. Business logic never thinks about freshness/caching.

3. **Input-based invalidation** - no TTL. Cache validity = input equality. Store what inputs were used to compute each cached value; on access, compare current inputs to stored inputs; same = valid, different = recompute.

4. **No "unknown" UI states** - wait for correct data, never show placeholders. If data is computable, compute it (wait if needed). If data is genuinely not computable (e.g., commit deleted), return an explicit truthful error, not "unknown".

5. **Parallel recompute** - when inputs change for multiple tasks, recompute in parallel (bounded pool ~8) to keep it fast.

6. **Durable "merged" status** - only from history events (`task.merged`), never inferred from integration cache.

7. **Immutable merged tasks** - store diff stats in `task.merged` history event at merge time, never recompute.

8. **Immutable closed tasks** - store diff stats in `task.closed` history event at close time, never recompute. This ensures that when base branch advances, only open tasks (1-10 typically) need recomputation, not the potentially large number of closed tasks.

9. **Backwards compatibility & seamless migration** - we MUST NOT break existing user setups (users are currently on v0.1.1). This applies to everything in this plan. Specifically:
   - Users with existing tasks (merged/closed/open) must continue to work correctly after upgrade.
   - A **one-time migration** should run automatically and seamlessly when the new binary first runs.
   - Migration must use **proper locking** to prevent corruption if multiple subtask processes run in parallel.
   - Migration should **backfill any missing data** (e.g., `base_commit` for existing tasks) to leave old tasks in an ideal state - the goal is to make old tasks indistinguishable from new ones.
   - **Migration must be isolated** - all migration logic belongs in a dedicated migration package, NOT spread around the codebase.
   - **Avoid backwards-compat branches in main code** - prefer backfilling/migrating old data to the new schema so the main codebase doesn't need conditionals or special cases for old vs new tasks. The migration does the work once so the rest of the code stays clean.

10. **Thorough e2e tests** - comprehensive end-to-end tests with actual golden fixtures covering various cases and edge cases. We need confidence that the implementation is correct and doesn't regress.

11. **External merge detection (ancestor-only)** - when a task branch tip becomes reachable from the base branch (via merge commit, fast-forward, or rebase), Subtask should detect this and write `task.merged` to history. This must:
   - Be **local-first** (no implicit `git fetch`; detection is based on refs present locally).
   - Write `task.merged` event with frozen stats (same as `subtask merge`).
   - Use ancestor detection only (`git merge-base --is-ancestor`) - no content detection.
   - Be correct or explicit error (e.g., branch deleted / base missing), never a placeholder.
   - **Not detect** squash merges or cherry-picks (matches GitHub behavior). For these, users see `open` with historical stats preserved.

## Non-requirements (things we're OK with)

1. **Waiting briefly for correct data** - if recompute is needed, we wait. Fast in practice because only 1-10 tasks change at a time and we recompute in parallel.

2. **Significant refactor** - we want ideal, correct, simple, and fast. Not a quick patch.

3. **One-off migration** - if needed to achieve backwards compatibility correctly, a well-designed migration is acceptable.
