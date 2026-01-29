# Merge Detection: Scenarios & Trade-offs

## The Dream

**For someone who doesn't know git:**
> "I created a task. The worker did the work. I reviewed it. I merged it. Done."

They never think about branches, commits, or merge strategies. They see:
- **Open** → work in progress
- **Merged** → work is in the codebase
- **Closed** → abandoned, didn't use it

**For someone who knows git:**
> "Subtask tracks my tasks. Workers do the work in isolated branches. I can merge however I want - via subtask, GitHub PR, manual git. Subtask figures it out."

They have freedom to use their preferred workflow. Subtask stays out of the way but keeps track.

---

### The Ideal Experience

1. **"Merged" means one thing:** The task's changes are in the main codebase now.
   - Doesn't matter *how* it got there
   - No "integrated", "detected", "indirect" - just "merged"

2. **Stats are permanent:** Once merged, you can always see what the task contributed (`+50 -10`). Like a GitHub PR - the stats don't disappear.

3. **No wrong states:** You never see a task as "open" when the work is already in main. You never see "merged" when it isn't.

4. **Simple lifecycle:**
   ```
   open → merged    (work shipped)
   open → closed    (abandoned)
   ```
   That's it. No weird transitions.

---

### What This Means Practically

| User Action | What Subtask Shows |
|-------------|-------------------|
| `subtask merge` | merged |
| Merge via GitHub PR | merged |
| `git merge` manually | merged |
| `git merge --squash` | merged |
| Cherry-pick the changes | merged |
| Close without merging | closed |

**The user never has to tell subtask "hey, I merged this externally."** Subtask just knows.

---

## Technical Exploration

This section explores how to achieve the dream above.

### Ancestor Detection (what GitHub uses)

```bash
git merge-base --is-ancestor <branchHead> <baseHead>
```

Returns true if the task branch tip commit is reachable from the base branch.

**Catches:**
- Merge commits (`git merge branch`)
- Fast-forward merges
- Rebase + push (task branch rebased onto base, then base fast-forwarded)
- `subtask merge` (which does squash + rebase + fast-forward)

**Misses:**
- Squash merge (`git merge --squash`) - creates new commit, branch tip not reachable
- Cherry-pick - creates new commit(s), branch tip not reachable

### Content Detection (catches squash/cherry-pick)

```bash
git merge-tree --write-tree <baseHead> <branchHead>
# Compare result tree to baseHead tree
```

Returns true if merging would produce no changes (content already in base).

**Catches everything above, plus:**
- Squash merge
- Cherry-pick

**Cost:** ~9ms per task vs ~4ms for ancestor-only.

---

### Scenario Comparison: Ancestor-Only vs Content Detection

| # | Scenario | Ancestor-Only | With Content Detection |
|---|----------|---------------|------------------------|
| | | Status / Changes | Status / Changes |
|---|----------|---------------|------------------------|
| A | **`subtask merge`** | `merged` / frozen ✓ | `merged` / frozen ✓ |
| B | **GitHub PR merge** | `merged` / frozen ✓ | `merged` / frozen ✓ |
| C | **Manual `git merge`** | `merged` / frozen ✓ | `merged` / frozen ✓ |
| D | **Fast-forward merge** | `merged` / frozen ✓ | `merged` / frozen ✓ |
| E | **Rebase + FF** | `merged` / frozen ✓ | `merged` / frozen ✓ |
| F | **Squash merge** | `open` / `+0 -0` | `merged` / frozen |
| G | **Cherry-pick** | `open` / `+0 -0` | `merged` / frozen |
| H | **Close without merging** | `closed` / frozen ✓ | `closed` / frozen ✓ |
| I | **Close, then branch merged** | `merged` / frozen ✓ | `merged` / frozen ✓ |
| J | **Close, then cherry-picked** | `closed` / frozen | `merged` / frozen |

### Analysis

**Scenarios A-E, H-I**: Both approaches behave identically. These cover the vast majority of workflows.

**Scenarios F-G (squash/cherry-pick while open)**:

| Aspect | Ancestor-Only | Content Detection |
|--------|---------------|-------------------|
| Status shown | `open` | `merged` |
| Changes shown | `+0 -0` (live) | frozen at detection |
| User signal | "Nothing left to merge" | "Merged" |
| False positives? | No | Possible (independent identical fix) |
| Matches GitHub? | Yes | No (GitHub stays Open) |
| Code complexity | Lower | Higher (+ Git version fallback) |

**Scenario J (close, then cherry-picked)**:

| Aspect | Ancestor-Only | Content Detection |
|--------|---------------|-------------------|
| Status shown | `closed` | `merged` |
| User intent | Preserved ("I closed it") | Overridden |
| Matches GitHub? | No (GitHub stays Closed) | No (GitHub stays Closed) |

### The Trade-off

| | Ancestor-Only | Content Detection |
|--|---------------|-------------------|
| **Correctness** | Never wrong | Risk of false positives |
| **Predictability** | High (matches GitHub) | Lower (magic detection) |
| **User intent** | Respected | Sometimes overridden |
| **Code complexity** | ~100 LOC | ~200+ LOC + fallback |
| **Git version** | Any | 2.38+ or fallback needed |
| **UX for squash** | `+0 -0` signals "done" | `merged` explicit |

### Open Questions

1. **Is `+0 -0` a good enough signal?** Users see "open" but zero changes - is that confusing or clear?

2. **Do we want to match GitHub?** GitHub's approach is battle-tested and users understand it.

3. **Is "too clever" detection risky?** Auto-marking merged when user didn't merge could surprise users.

---

### What Users See

| Status | Changes Field | Notes |
|--------|---------------|-------|
| `open` | Live diff (`+50 -10`) | Updates as worker makes changes |
| `merged` | Frozen at merge (`+50 -10`) | Preserved forever |
| `closed` | Frozen at close (`+50 -10`) | Preserved forever |

---

## Implementation Cost

| Approach | Lines of Code | Catches |
|----------|---------------|---------|
| Current implementation | ~1800 | All merge types |
| Proposed rewrite | ~150-200 | All merge types |

The current implementation has ~1600 lines of accidental complexity (snapshots, repair passes, multiple strategies, promotion logic). The core detection is simple.

---

## Recommendation

To achieve "the dream":

1. **Use ancestor + content detection** - catches all merge styles
2. **Auto-write `task.merged` to history** - freezes stats, marks task done
3. **Closed can become merged** - matches GitHub, more forgiving

This means rewriting the current ~1800 line implementation as ~150-200 lines.
