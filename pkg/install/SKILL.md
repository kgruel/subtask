---
name: subtask
description: "Parallel task orchestration CLI that dispatches work to AI workers (via Claude Code) in isolated git workspaces. Use when the user wants to draft, create, run, or manage tasks, delegate tasks to workers/subagents, or mentions subtask or Subtask."
---

# Subtask

Subtask dispatches AI workers to isolated git worktrees so they can work in parallel without conflicting. There are three roles: the **user** who gives direction, **you** (the lead) who orchestrates, and **workers** who execute. The user tells you what they need; you draft tasks, dispatch, review, and decide when to merge.

Two primitives drive the system. **Routines** shape the workflow (a named sequence of steps). **Agents** identify the worker and its dispatch config (adapter + model + reasoning, plus an optional role prompt). Read sections [Routines](#routines) and [Agents](#agents) before drafting; then the [Flow](#flow) section is just shell commands.

## Mindset and narration

- **Understand before delegating.** Don't rush to draft until the user's intent is clear.
- **Own the complexity.** Track every task in flight; surface progress and blockers; don't make the user chase you.
- **Work autonomously between user inputs.** Review worker output, request changes, iterate. Escalate only decisions the user must make (merge/close, scope changes, design tradeoffs).
- **Pace parallelism to your review bandwidth.** Worker time runs in parallel; your review is serial. ~2–3 tasks in flight is the practical cap.
- **Narrate sparingly.** The user reads your tool calls. Surface decisions, blockers, batch milestones. Stay silent on stage transitions, routine progress, and routine findings you can address yourself.

## Routines

A routine is a named sequence of steps. Each step's `instructions:` field prints when you run `subtask stage`, telling you what to do next — so **this SKILL does not duplicate per-step instructions**; the routine YAML does.

**Canonical routines** (built-in, no config needed):

- `default` — `doing → review → ready`. Direct execution; you review the diff at the end.
- `they-plan` — `plan → implement → review → ready`. Worker drafts PLAN.md; you review and approve before they implement.
- `you-plan` — `plan → implement → review → ready`. You draft PLAN.md; worker pokes holes before they implement.

**How to pick.** Use `default` for direct execution. Use `they-plan` when the work is non-trivial and the worker should drive the plan. Use `you-plan` when you have strong opinions about scope and want the worker to challenge them.

Pick at draft time:

```bash
subtask draft fix/bug --routine they-plan --base-branch main --title "Fix worker pool panic" <<'EOF'
There's an intermittent panic in pool.go under high concurrency.
EOF
```

`subtask draft` also accepts `--follow-up <task>` to carry conversation context forward from a prior task (useful for chained work; rarely needed).

**Project routines** live at `.subtask/routines/<name>.yaml` and override or extend the canonical set. List what's available with `subtask routines` (`--json` for tooling). Minimal shape:

```yaml
name: ship-it
description: Build, test, commit, push.
default_prompt:
  text: |
    Run `make check` before committing. Use conventional-commit messages.
steps:
  - id: build
    instructions: |
      Tell the worker to build and test. When green:
        `subtask stage <task> commit`
    worker_instructions: |
      Run `make build && make test`. Report failures verbatim.
  - id: commit
    notify: false
    worker_instructions: Commit your work with a conventional-commit message.
  - id: done
    kind: terminal
    instructions: Notify the user.
```

A few fields worth knowing:

- **`default_prompt.text`** — project-wide brief that rides on every worker prompt for this routine. Use for regen recipes, commit conventions, "fix the cause not the test." The canonical routines ship a PROGRESS.json brief; your project's override replaces it.
- **`notify: false`** on a step suppresses the unread-reply nudge for replies in that step. Use for mechanical bookkeeping transitions (committing, snapshot regeneration) where worker replies aren't worth your attention.
- **`worker_instructions:`** on a step (or `agent:` on the step, or a positional prompt to `subtask stage`) makes `subtask stage <task> <step>` auto-dispatch to the worker. Without any of those triggers, `stage` is passive — `send` next.

Unknown step or routine YAML keys fail loud at load — trust the error rather than guessing the schema.

## Agents

An agent is a named dispatch config: adapter + model + reasoning, plus an optional role-defining prompt. Use `--agent <name>` on `draft` to select one. `--agent` is mutually exclusive with `--routine` — routine tasks set per-step agents via `agent:` in the routine YAML, not via `--agent` on `draft`.

```bash
subtask draft refactor/auth --agent planner --base-branch main --title "..." <<'EOF'
...
EOF
```

Agent files live at `.subtask/agents/<name>.yaml`. List with `subtask agents` (`--json` available). Minimal shape:

```yaml
description: Drafts surgical PLANs; pushes back on over-engineering.
adapter: claude
model: claude-opus-4-7
reasoning: high
prompt:
  text: |
    You write minimal, reversible PLAN.md files. Prefer composition over invention.
```

`prompt:` is optional (use when the worker plays a specific role: planner, reviewer, fixer). It must have exactly one of `text:` or `file:` (the latter relative to `.subtask/`). An agent's adapter/model/reasoning overlays project config. Bad agent YAML errors at load.

**Per-task vs per-prompt.** `--agent` on `draft` persists into the TASK.md snapshot — every subsequent `send`/`stage`/`review --task` inherits it. `--agent` on `send`/`review`/`ask` is ephemeral and applies only to that invocation.

**Resolution order:** explicit flag (`--adapter`/`--model`/`--reasoning`) → `--agent` overlay → task snapshot (when a task is in scope) → project config → global config.

**Snapshot semantics.** Editing `.subtask/config.json` after `draft` does *not* retroactively update existing tasks. To run a one-off with a different agent, pass `--agent` to `send`/`review`/`ask`. To swap automatically on a step transition, bind `agent:` on that step in the routine YAML — the harness applies it persistently into the snapshot.

**Cross-adapter swap clears the session.** When a routine-bound agent swap crosses adapter families (e.g. claude → codex), the harness clears the worker session. Cross-step context comes from the workspace + PLAN.md + PROGRESS.json — file-based collaboration, not session carry-over. That's why the swap is safe.

**Reviewer agent (principle, not recipe).** For `subtask review --task`, pick an agent whose adapter family **differs from the worker's** for blind-spot coverage. Use `subtask agents` to see what your project ships; the SKILL deliberately doesn't name agents — projects diverge.

## Flow

Shell commands only. Don't expect this section to re-explain primitives — that's [Routines](#routines) and [Agents](#agents).

When unsure where a task stands, run `subtask next <task>`.

```bash
# 1. Draft (task name is branch name; description goes via heredoc/stdin)
subtask draft fix/bug --routine default --base-branch main --title "Fix worker pool panic" <<'EOF'
Intermittent panic in pool.go under high concurrency. Reproduce, fix, add tests.
EOF

# 2. Dispatch the worker. run_in_background: true on the Bash tool — no & in the command.
subtask send fix/bug "Go ahead."

# 3. When notified the bash exited, read the reply from durable history.
subtask reply fix/bug

# 4. Advance to review and inspect the workspace diff.
subtask stage fix/bug review
subtask diff --stat fix/bug
subtask diff fix/bug

# 5. Cross-adapter review pass (pick a reviewer whose adapter differs from the worker's).
subtask review --task fix/bug --agent <reviewer>

# 6. Address findings; re-review after substantive fixes.
subtask send fix/bug "Please handle the empty-pool edge case too."
subtask review --task fix/bug --agent <reviewer>

# 7. Hand off to the user for the merge/close decision.
subtask stage fix/bug ready
subtask merge fix/bug -m "Fix race condition in worker pool"
# or, if not merging:
subtask close fix/bug
```

## Meta surfaces

Commands no routine prints. Use these when the lead loop needs them.

- `subtask list` — every task and its status (`-a` includes closed; `--json` for tooling).
- `subtask quickstart` — state-aware onboarding: first-task flow when empty, task entry points when work exists.
- `subtask next <task>` — state-aware next-command cue for a specific task.
- `subtask show <task>` — task detail, progress, worker status.
- `subtask diff [--stat] <task>` — workspace diff against the base branch. See gotcha 1 below before relying on `send` reply summaries.
- `subtask log <task>` — full task conversation and lifecycle events.
- `subtask trace <task>` — worker tool calls and internal state, for debugging stuck or misbehaving runs.
- `subtask interrupt <task>` — gracefully stop a running worker.
- `subtask unread` — list open tasks with worker replies you haven't read (exits non-zero if none).
- `subtask workspace <task>` — print the git worktree path.
- `subtask ask "..."` — one-off question with no task, runs in cwd. Passthrough to the configured adapter; useful for quick lookups (`--agent` to override). Not a task primitive.
- `subtask review` standalone forms — `--base BRANCH` (PR-style), `--uncommitted` (staged + unstaged + untracked), `--commit SHA`, `--plan` (with `--task`, reviews PLAN.md against TASK.md instead of the diff).

## Gotchas

1. **`send` reply is not the code diff.** The "Changed:" summary in a `send` reply reflects task-folder files (PLAN.md, PROGRESS.json). For workspace code changes use `subtask diff <task>` (or `--stat`). You are blind to code from the reply alone.
2. **Two-send pattern for plan-approved → implement.** After approving PLAN.md in a `*-plan` routine, run `subtask stage <task> implement` *first*, then `subtask send <task> "..."` separately. Never bundle "approved, now implement" in one message — workers execute against the step they're currently in.
3. **PROGRESS.json is symlinked, not committable.** The task folder is symlinked into the worktree; `git add .subtask/tasks/<name>/PROGRESS.json` errors with "pathspec ... is beyond a symbolic link." Workers commit code; PROGRESS.json is lead-side bookkeeping and travels with the portable task folder.
4. **Cross-adapter review is a practice, not optional polish.** Same-family review (Claude reviewing Claude) misses what a different family catches. Make `subtask review --task --agent <different-family>` part of the review step every time — not a bonus pass when you have time.
5. **`send` blocks; `stage` sometimes blocks.** `subtask send` is synchronous — run it with `run_in_background: true` on the Bash tool call. Do not add `&` to the command itself; that backgrounds the subprocess in the shell and the Bash tool exits immediately (exit 0), which defeats the notification — you'll think the send is done when the worker hasn't even started. Don't pipe it through `tail`/`head`; use `-q` for quiet output and `subtask reply` to fetch the canonical reply from history. `subtask stage` also blocks when the target step auto-dispatches (see Routines for the triggers); pass `--no-send` to stay passive.
