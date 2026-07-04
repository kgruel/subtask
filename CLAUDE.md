# Subtask

Parallel task management and orchestration for AI coding agents.

A lead agent dispatches work to parallel workers in isolated git worktrees, with context preservation and progress tracking.

---

## Philosophy

### The Problem

Without Subtask, a user managing multiple AI coding sessions must:
- Open multiple terminal tabs, each running Claude Code or Codex
- Mentally track what each session is working on
- Juggle progress, blockers, and dependencies in their head
- Re-explain context when sessions crash or expire
- Be the project manager while also trying to get work done

This cognitive load is exhausting. The user becomes a human orchestrator for AI workers.

### The Solution

Subtask shifts the cognitive burden from user to lead agent.

**Three roles:**
- **User**: Gives direction, makes decisions, approves work
- **Lead** (e.g., Claude Code): Orchestrates everything in between
- **Workers**: Execute tasks in parallel, isolated in git worktrees

The user provides direction. The lead handles everything else: understanding requirements, breaking work into tasks, dispatching to workers, tracking progress, reviewing output, iterating until it's right, merging when ready.

The lead is not a task dispatcher. The lead is a technical lead / project manager. We're building tools that enable this.

### Design Goals

Every feature should ask:

1. **Does this reduce cognitive load on the user?**
2. **Is this simple for the lead to use and understand?**

If a feature adds complexity for the user, it's probably wrong. If it confuses the lead, it's also wrong. Internal complexity is fine if the interface stays simple.

---

## Design Decisions

1. **Task-centric** — Task name is the primary identifier. Everything flows from it.
2. **Git-native** — Branches for isolation, worktrees for parallelism, standard merge workflow.
3. **File-based collaboration** — Task folder shared between lead and worker. PLAN.md for plans, PROGRESS.json for tracking. Files persist; sessions don't.
4. **Workspace opacity** — Lead never picks workspaces. Subtask assigns them. Isolation is git-level only: separate worktrees, separate branches. Runtime resources (ports, services, databases) are project-managed and shared by default — projects are responsible for any per-worker offsetting.
5. **Context preservation** — Task folders are the portable, syncable unit. history.jsonl and `--follow-up` ensure nothing is lost when sessions crash. Copy anywhere, full context. Internal state and caches are local and rebuildable.
6. **Progress visibility** — Tool counts, timing, and PROGRESS.json let lead track workers without interrupting them.
7. **Errors at subtask's own boundaries are actionable** — Where subtask can name the recovery — an adapter override path, an install command, a config field, a workspace state — the error says so rather than relaying raw upstream stderr. Pass-through is fine when subtask has nothing specific to add (e.g., raw git or harness errors).
8. **Destructive operations require explicit intent** — Anything that loses work (`close --abandon`, force-resets, overwrites) needs a flag or confirmation. Defaults preserve.
9. **Local-first** — Operations use local git state. Lead controls when to sync with remote. No stale remote ref surprises.
10. **Event sourcing** — `history.jsonl` is the append-only source of truth. SQLite index is a derived projection for fast queries. If they diverge, history wins.

---

## Concepts

### Lead vs Worker

**Lead** (e.g., Claude Code, Codex, OpenCode):
- Runs `subtask` CLI commands
- Sees CLI output (`list`, `show`, errors)
- Drafts tasks, sends prompts, reviews work
- Reads task folders and history.jsonl

**Worker** (e.g., Codex, Claude):
- Receives prompts via harness
- Sees the repository and task folder
- Updates PROGRESS.json
- Never sees CLI output or other tasks

### Task

A named unit of work: `fix/epoch-boundary`
- Folder at `.subtask/tasks/<name>/` (with `/` escaped as `--`)
- Contains TASK.md (description), optional PLAN.md, PROGRESS.json
- Symlinked into workspace for lead/worker collaboration

### Workspace

Isolated git worktree where tasks execute. Created on-demand from a configured pool. Lead never picks workspaces—subtask assigns them.

### Harness

Worker backend that executes prompts. Built-in adapters: `codex`, `claude`, `opencode`, `gemini`, `pi`. Configured in `.subtask/config.json`. See [docs/adding-an-adapter.md](docs/adding-an-adapter.md) for adding a new one.

### Routines

A **routine** is a named sequence of steps that structures how a task moves from start to finish. Routines replace the legacy workflow system.

**Canonical routines** (built-in, no config needed):
- `default`: `doing → review → ready` — plain execution, lead reviews diff
- `they-plan`: `plan → implement → review → ready` — worker drafts PLAN.md, lead approves
- `you-plan`: `plan → implement → review → ready` — lead drafts PLAN.md, worker reviews

Use `--routine <name>` on `subtask draft` to select one:
```
subtask draft fix/bug --routine they-plan --title "..."
```

Routines are project-extensible. Drop a YAML at `.subtask/routines/<name>.yaml` to override a built-in or add a custom one. Each step can optionally:
- Bind an `agent:` to swap the harness on transition (also triggers auto-dispatch on `subtask stage`)
- Set `worker_instructions:` to inject a brief into the worker prompt (triggers auto-dispatch on `subtask stage`)
- Set `worker_context:` for passive per-step context that rides along without triggering dispatch
- Set `notify: false` to suppress unread-reply nudges while the task is in that step

**`subtask stage` auto-dispatches when the target step has `agent:`, `worker_instructions:`, or when a positional prompt is passed to `stage`** — it writes the new step, applies any agent (clearing the session on cross-adapter swaps), appends a history event, and then sends to the worker and blocks until reply. When none of those triggers are present, `stage` is passive and the lead must `subtask send` next. Pass `--no-send` to stay passive even when a trigger is defined. An optional positional prompt extends the worker_instructions (or dispatches on its own when `worker_instructions:` is absent).

**Adapter/model snapshot in TASK.md.** When a task is drafted, the resolved adapter, model, and reasoning are written into TASK.md frontmatter (the "snapshot"). This is intentional — design principle #5, portability. `send`, `review --task`, and `stage` all resolve adapter/model from the snapshot first, falling back to project config. Editing `.subtask/config.json` after a task is drafted does **not** retroactively update the snapshot. To run with a different agent for a one-off `send` or `review` without editing TASK.md, pass `--agent <name>`. `stage` has its own agent mechanism: bind `agent:` to the target step in the routine YAML, and the harness swaps automatically (and persistently, into the snapshot) on transition.

### Status & Transitions

**Task Status** (organizational, durable — stored in `history.jsonl`):
| Status | Meaning |
|--------|---------|
| `open` | Task is active |
| `merged` | Work merged into base branch |
| `closed` | Closed without merging |

`subtask show` renders a derived **User Status** — see below.

Transitions:
- `open` → `merged` (via `merge`)
- `open` → `closed` (via `close`)
- `merged` → `open` (via `send`—revives to fix issues)
- `closed` → `open` (via `send`—revives with new workspace)

**Worker Status** (ephemeral, within an open task):
| Status | Meaning |
|--------|---------|
| `idle` | No worker activity yet |
| `working` | Worker currently executing (`running` accepted as legacy alias on read) |
| `replied` | Worker finished, awaiting follow-up |
| `error` | Last run failed |

Transitions:
- `idle` → `working` (via `send`)
- `working` → `replied` (worker finishes) or `error` (failure)
- `replied` → `working` (via `send`)
- `error` → `working` (via `send`)

**User Status** (derived, what `subtask show` and `subtask list` render):
| Status | Meaning |
|--------|---------|
| `draft` | Task created but not yet dispatched (open + worker never started) |
| `working` | Worker currently executing |
| `replied` | Worker finished, awaiting lead follow-up |
| `error` | Last worker run failed |
| `merged` | Work merged into base branch |
| `closed` | Closed without merging |

Task status is what users care about. Worker status is operational detail. Workspace stays with task until closed/merged.

---

## Commands

| Command | Description |
|---------|-------------|
| `subtask install` | One-time global install + configuration wizard |
| `subtask uninstall` | Remove the installed skill |
| `subtask status` | Show installation status |
| `subtask config` | Edit user defaults or project overrides |
| `subtask draft <task>` | Create a task without running it |
| `subtask send <task>` | Send a message (starts or resumes task; blocks until reply). `--detach` returns once a detached supervisor claims the task; retrieve the reply with `subtask reply` after `subtask wait` |
| `subtask reply <task>` | Print the most recent worker reply (from history) |
| `subtask next <task>` | Show the state-aware next command for a task |
| `subtask stage <task> <step>` | Advance routine step |
| `subtask routines` | List available routines (canonical and project) |
| `subtask agents` | List project agents |
| `subtask list` | Show all tasks and workspaces |
| `subtask show <task>` | Task details, progress, diff stats |
| `subtask unread` | List open tasks with unread worker replies (exit 1 if none) |
| `subtask wait <task>...` | Block until named tasks finish (completion barrier) |
| `subtask diff <task>` | Show task diff |
| `subtask merge <task> -m "..."` | Squash-merge into base branch, close |
| `subtask close <task>` | Close without merging (`--abandon` discards changes) |
| `subtask workspace <task>` | Print workspace path |
| `subtask ask "..."` | Quick question (no task, runs in cwd) |
| `subtask quickstart` | Show a state-aware onboarding cue (first-task flow or next-step pointers) |
| `subtask interrupt <task>` | Gracefully stop a running worker |
| `subtask log <task>` | Show conversation and lifecycle events |
| `subtask trace <task>` | Debug worker runs (tool calls, errors) |
| `subtask review <task>` | Get an AI code review |
| `subtask update` | Update subtask to the latest release |

---

## Storage

```
~/.subtask/
├── config.json                              # global defaults (from install/config)
├── workspaces/<escaped-git-root>--<id>/     # worktrees (created on demand)
└── projects/<escaped-git-root>/             # per-project runtime state (machine-local)
    ├── internal/                            # session IDs, workspace assignments, locks
    └── index.db                             # SQLite cache (rebuildable)

<repo>/.subtask/
├── config.json                              # optional per-project overrides
└── tasks/<name>/                            # task folder (portable, syncable)
    ├── TASK.md                              # description + schema version in frontmatter
    ├── PLAN.md                              # optional plan
    ├── PROGRESS.json                        # worker progress tracking
    └── history.jsonl                        # source of truth: messages + lifecycle events
```

**Claude Code install paths** — `subtask install` writes both:
- `~/.claude/skills/subtask/SKILL.md` — the Claude Code skill (teaches Claude the subtask workflow)
- `~/.claude/plugins/subtask/` — the plugin (hooks: unread-reply guard, stale-worker warning)

The plugin directory carries a `.subtask-binary-installed` marker so `subtask uninstall` can distinguish it from a marketplace install (`/plugin install subtask@subtask`), which is left alone. Use `--skill-only` on either command to skip plugin operations.

### Portability Contract

**Task folder** (`.subtask/tasks/<name>/`) is portable and syncable:
- Copy to another machine, sync via git/syncthing, back up
- Contains everything needed to understand and resume the task
- `history.jsonl` is the source of truth for task status, stage, messages

**Internal folder** (`~/.subtask/projects/<escaped-git-root>/internal/<name>/`) is runtime-only:
- Workspace paths, session IDs—machine-specific
- Rebuilt automatically when needed
- Never sync or back up

**SQLite index** is a local cache:
- Fast queries for `list` and TUI
- Rebuilt from task folders if missing
- Never sync—it's a projection, not source data

---

## Project Layout

```
.
├── cmd/subtask/             # CLI commands and main.go entry point
├── pkg/                     # importable packages
│   ├── task/                # Task/State structs, paths, locking, progress
│   │   ├── store/           # shared read/list/view layer over the index (used by CLI and TUI)
│   │   ├── history/         # history.jsonl: append, read, tail
│   │   ├── index/           # SQLite index for fast list/TUI queries
│   │   ├── migrate/         # schema migrations (legacy → current)
│   │   └── ops/             # task operations (merge, close)
│   ├── tui/                 # interactive TUI (Bubble Tea)
│   ├── workspace/           # workspace pool allocation
│   ├── harness/             # YAML-driven adapters for worker CLIs
│   │   └── adapters/        # built-in adapters: codex, claude, opencode, gemini, pi
│   ├── git/                 # git operations (branches, merge, diff, worktrees)
│   ├── render/              # CLI output formatting (TTY detection, colors)
│   ├── routine/             # routine definitions (YAML, embedded canonicals)
│   ├── logs/                # session log parsing and formatting
│   ├── logging/             # subtask's own logger
│   ├── diffparse/           # parse `git diff` output
│   ├── subtaskerr/          # typed errors with recovery hints
│   ├── install/             # skill install + embedded SKILL.md
│   ├── testutil/            # test helpers (isolated environments)
│   └── e2e/                 # integration tests
├── internal/                # private packages (not importable)
│   ├── binaryupdate/        # self-update logic
│   ├── filelock/            # cross-platform file locks
│   └── homedir/             # ~ expansion across OSes
├── plugin/                  # Claude Code plugin assets (hooks, scripts)
├── .claude-plugin/          # plugin marketplace metadata
└── scripts/                 # install scripts (install.sh, install.ps1)
```

---

## Working on This Codebase

### Build & Test

```bash
go build ./cmd/subtask
go test ./... -short      # fast
go test ./...             # includes e2e
```

### Adding a Command

1. Create `cmd/subtask/<cmd>.go`
2. Define struct with kong tags
3. Implement `Run() error`
4. Add to CLI struct in `main.go`

### Adding a Harness

Adapters are YAML-driven. See [docs/adding-an-adapter.md](docs/adding-an-adapter.md) for the full guide. In short:

1. Create `pkg/harness/adapters/<name>.yaml` describing the CLI invocation.
2. If the CLI emits a custom JSON event format, add a parser in `pkg/harness/parse.go` and register it in `ParseByName` and `parseOutput`.
3. If sessions need migration when workspaces move, add a handler in `pkg/harness/session_handlers.go`.

### Releasing

The binary version (`cmd/subtask/main.go`) and the plugin version (`.claude-plugin/marketplace.json` and `plugin/.claude-plugin/plugin.json`) are coupled — they always match. Users installing the binary at version X expect the same-version plugin from the same commit, and Claude Code surfaces the plugin version in its UI.

To bump:

```bash
scripts/bump-version.sh 0.4.0     # updates all three files
git commit -am "Bump version to 0.4.0"
git tag v0.4.0
git push fork main && git push fork v0.4.0
```

Never edit one of the three version sites without the others.
