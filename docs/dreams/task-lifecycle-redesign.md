# Task Lifecycle Redesign

> **Related research:** See task `research/git-workflow-design` which analyzed current Git behavior and proposed improvements (merge-base diffs, commit trailers, activity log). The plan is at `.subtask/tasks/research--git-workflow-design/PLAN.md`. Consider continuing with that worker to refine this design.

---

## The Problems

### Too Many States

Current states: `draft`, `working`, `replied`, `error`, `closed` (plus hidden `Merged` flag)

These conflate two different things:
- **Task state** — is this work done?
- **Worker state** — is someone working on it right now?

`working` and `replied` describe the worker, not the task. That's confusing.

### Merged Tasks Are Sealed

When a task is merged:
- Branch is deleted
- Task is marked closed
- Can't easily continue the conversation

Common case: you merge, then realize you want one more tiny change.

Current flow for a follow-up after merge:
```bash
subtask draft fix/bug-v2 --follow-up fix/bug --base-branch main --title "Add docstring"
subtask send fix/bug-v2 "Add a docstring to that function"
subtask merge fix/bug-v2 -m "Add docstring"
```

The friction is conceptual — "add a docstring" isn't a new task, it's the same task, one more thing.

### Closed vs Merged Confusion

"Closed" can mean:
- Closed but not merged (branch may exist, can revive)
- Closed and merged (branch gone)

These behave differently but look the same. The `Merged` flag is hidden.

### Tasks Should Be Permanent Records

Tasks are valuable records:
- What was the goal (TASK.md)
- What was discussed (history.jsonl)
- Context for follow-up work
- Historical reference

Even abandoned work is valuable. Don't delete it.

---

## The Solution: Separate Task and Worker

**Task** = the work to be done (permanent record, conversation)
**Worker** = the agent doing the work (ephemeral, runs and replies)

Currently we conflate them. Separating them simplifies everything.

---

## Task States

| State | Meaning |
|-------|---------|
| **open** | Active work (branch exists) |
| **merged** | Work landed, at rest (can reopen) |
| **closed** | Done without merge (abandoned/discussion only) |

**That's it.** Three states, clear meanings.

### Key insight: Merged is a resting state, not a sealed tomb

You can always reopen a merged task by sending to it:
- Fresh branch created from current main
- Conversation continues
- No ceremony, no new task name

---

## Worker States (Separate)

| State | Meaning |
|-------|---------|
| `running` | Worker is executing |
| `replied` | Worker finished, awaiting your response |
| `error` | Worker crashed |
| `-` | No worker (task at rest) |

Worker state is shown separately from task state. They're orthogonal.

---

## How It Works

```bash
subtask draft fix/bug "Fix the race condition"
# Task: open, Worker: -

subtask send fix/bug "Implement the fix"
# Task: open, Worker: running
# ... worker works ...
# Task: open, Worker: replied

subtask send fix/bug "Also add a test"
# Task: open, Worker: running
# ... worker works ...
# Task: open, Worker: replied

subtask merge fix/bug -m "Fix race condition"
# Task: merged, Worker: -
# Branch deleted, work is in main

# Later: "oh, one more thing"
subtask send fix/bug "Add a docstring"
# Task: open (reopened!), Worker: running
# Fresh branch from main, conversation continues
# ... worker works ...
# Task: open, Worker: replied

subtask merge fix/bug -m "Add docstring"
# Task: merged, Worker: -
# Back to resting state

subtask close fix/bug
# Task: closed, Worker: -
# Done thinking about this (optional - merged tasks can stay merged)
```

---

## What You See

```
subtask list

TASK              STATUS      WORKER
fix/bug           open        running
feat/auth         open        replied
refactor/cleanup  ✓ merged    -
docs/readme       closed      -
```

- **STATUS**: task state (open/merged/closed)
- **WORKER**: what's happening right now (running/replied/error/-)

Clean separation. No "landed 2x" counts cluttering the view.

---

## Where History Lives

History doesn't belong in the list view. It lives in:

### Storage: JSONL (Single Source of Truth)

```jsonl
{"ts":"...","type":"task.drafted"}
{"ts":"...","type":"message","role":"lead","content":"Implement the fix"}
{"ts":"...","type":"message","role":"worker","content":"Done. Added mutex and tests."}
{"ts":"...","type":"task.merged","commit":"abc123","into":"main"}
{"ts":"...","type":"message","role":"lead","content":"Add a docstring"}
{"ts":"...","type":"message","role":"worker","content":"Done."}
{"ts":"...","type":"task.merged","commit":"def456","into":"main"}
```

Machine-parseable. Messages and lifecycle events in one stream.

### Interface: CLI Commands

Agents and humans use commands, not raw files:

```bash
subtask log fix/bug              # full conversation + events
subtask log fix/bug --events     # lifecycle events only (compact)
subtask log fix/bug --messages   # messages only
subtask log fix/bug --since=1d   # recent activity
subtask trace fix/bug            # debugging (tool calls, errors)
```

**Naming (following CLI conventions):**
- `log` — conversation + lifecycle (like `git log`)
- `trace` — low-level debugging (tool calls, why things failed)

**Why commands over files:**
- Git does this: `git log`, not `cat .git/objects/...`
- Single source of truth (JSONL), rendered for consumption
- Flags enable filtering (`--events` answers "when did we merge?")
- No sync issues between multiple files

### Git Log (with trailers)

Every squash commit includes:
```
Fix the race condition

Subtask-Task: fix/bug
```

Trace any commit back to its task: `git log --grep="Subtask-Task: fix/bug"`

---

## State Transitions

```
draft ──send──→ open ──merge──→ merged
                  ↑                │
                  └────send────────┘

open ──close──→ closed
merged ──close──→ closed (optional, merged can stay merged)
closed ──send──→ open (revive)
```

- **send** to any task opens it (creates branch if needed)
- **merge** lands work and puts task at rest
- **close** is organizational ("I'm done thinking about this")

---

## Errors

Worker crashes? That's a worker state, not a task state:

```
TASK      STATUS    WORKER
fix/bug   open      error
```

The task is still open. Send again to retry:
```bash
subtask send fix/bug "Try again, and watch out for X"
```

---

## Commands

| Command | Effect |
|---------|--------|
| `subtask draft <task>` | Create task (status: open) |
| `subtask send <task> "..."` | Send message (opens if needed, starts worker) |
| `subtask merge <task> -m "..."` | Land work (status: merged) |
| `subtask close <task>` | Mark done (status: closed) |
| `subtask show <task>` | See status, diff stats, worker state |
| `subtask log <task>` | Conversation + lifecycle events |
| `subtask trace <task>` | Debugging (tool calls, errors) |
| `subtask list` | See all tasks |

No "pause", "revive", "reopen" commands. Just send to anything.

---

## Comparison

| Old | New |
|-----|-----|
| draft, working, replied, error, closed | **open, merged, closed** (task) + **running, replied, error** (worker) |
| "revive" a task | just send to it |
| explicit worker management | worker state is just a column |
| Merged flag (hidden) | merged is a real status |
| Can't continue after merge | send reopens merged tasks |
| New task for follow-ups | same task, conversation continues |

---

## Git Workflow (From Research)

### Diffs Use Merge-Base

Always compute diff as:
```
diff_base = git merge-base <task-branch> <base-branch>
diff = diff_base..HEAD
```

Never use stored `base_commit`. This fixes "stale diff after rebase."

### Commit Traceability

Every squash commit includes trailer:
```
Fix the race condition

* Add mutex to protect shared state
* Add tests

Subtask-Task: fix/bug
```

---

## Open Questions

1. Should `subtask list` hide merged/closed tasks by default? (`--all` to show)
2. How to handle concurrent sends to the same task?
3. Should closing a merged task be automatic after some idle time?
4. When reopening a merged task, should we require explicit confirmation?
