# Feature: Support OpenCode as lead (Plan)

**Note (2026-01-16):** Subtask no longer distributes lead-agent skills to OpenCode skill locations. This plan remains as historical research; the supported lead skill distribution target is **Claude Code** (`~/.claude/skills/subtask/SKILL.md`) via `subtask install|uninstall|status`.

Source of truth for this task remains:
`.subtask/tasks/feature--opencode-lead/PLAN.md`

This file is a tracked copy so the plan is reviewable in git without relying on the runtime `.subtask/` directory (which is normally gitignored).

---

## 0) TL;DR

OpenCode’s “no background terminals” limitation is solvable via **OpenCode’s plugin system**: a plugin can start `subtask send` in the background, then **inject the worker reply back into the same OpenCode session** using OpenCode’s `session.prompt_async` API (and optionally show a TUI toast). This gives:

- Lead does not block while worker runs
- Automatic notification on completion
- Automatic continuation without human intervention (OpenCode receives a new message and replies)

## 1) Research Findings (OpenCode architecture)

### 1.1 There are two “OpenCode” repos; only one matches opencode.ai docs

1) `anomalyco/opencode` (TypeScript, Bun, monorepo; homepage `https://opencode.ai`)
- Client/server architecture (multiple clients: TUI/desktop/web)
- First-class **plugins**, **skills**, **custom tools**, **MCP servers**
- Has an **async session prompt endpoint** (`session.prompt_async`) and **TUI toast endpoint** (`tui.show-toast`)

2) `opencode-ai/opencode` (Go, bubbletea; small repo)
- Has MCP config in schema, but code is missing many features present in opencode.ai docs (e.g., skills/plugins)
- MCP integration appears short-lived per call (no persistent notification-driven workflow)

For Subtask “lead agent” support, the plan below targets **`anomalyco/opencode`**, since it’s the active project behind opencode.ai and already exposes the primitives we need.

### 1.2 Extension points available in `anomalyco/opencode`

**Plugins** (recommended)
- Docs: `https://opencode.ai/docs/plugins/`
- Loaded automatically from:
  - Project: `.opencode/plugin/*.ts|*.js`
  - Global: `~/.config/opencode/plugin/*.ts|*.js`
  - Or via config `plugin: ["npm-package", ...]`
- Plugin context includes:
  - `client` (OpenCode SDK client; can call server endpoints)
  - `serverUrl`
  - `$` (Bun shell API to run commands)
- Plugins can:
  - Define **custom tools** (`hooks.tool`)
  - Listen to events (`hooks.event`)
  - Intercept tool execution (`tool.execute.before/after`)

**Skills**
- Docs: `https://opencode.ai/docs/skills/`
- Discovered via OpenCode’s skill discovery locations (project + global), plus Claude-compatible `.claude/skills/...`
- Loaded on demand via OpenCode’s built-in `skill` tool.

**Custom Tools**
- Docs: `https://opencode.ai/docs/custom-tools/`
- Discovered at:
  - `.opencode/tool/*.ts|*.js` (project)
  - `~/.config/opencode/tool/*.ts|*.js` (global)
- Great for adding a single tool, but **plugins** are better for background notifications + session injection.

**MCP servers**
- Docs: `https://opencode.ai/docs/mcp-servers/`
- OpenCode supports MCP transports and handles MCP notifications (e.g. tool list changed), but using MCP alone doesn’t solve “inject a message back into a session” unless paired with a plugin.

### 1.3 “Async continuation” primitives in OpenCode

OpenCode exposes an async prompt API (`session.prompt_async`) that returns immediately while starting the agent run in the background, and has a TUI toast API (`tui.show-toast`). A plugin can call these APIs via the provided SDK client.

This is the critical difference vs “background terminal” dependence: we can implement async notifications **inside OpenCode**, not in the terminal emulator.

## 2) Problem Restatement (what must be true)

When OpenCode is the lead:
- `subtask send` must not block the OpenCode agent session/UI
- When the worker finishes, OpenCode must:
  1) notify the user (nice UX)
  2) inject the worker reply into the same session
  3) trigger an agent response automatically (no human “go run/show/paste” step)

## 3) Proposed Solution (recommended)

### 3.1 Ship an OpenCode plugin: `opencode-subtask`

Implement a plugin that provides a custom tool, e.g. `subtask_send`:

**Tool: `subtask_send`**
- Inputs:
  - `task` (string)
  - `prompt` (string; allow multi-line)
  - optional: `stage`, `follow_up`, `model_override`, etc. (later)
- Behavior:
  1) Ask permission (OpenCode permission system) for running Subtask.
  2) Start `subtask send <task>` **in the background** (do not await).
     - Redirect stdout/stderr to a log file under `.subtask/internal/<task>/opencode.log` to avoid noisy UI.
  3) Return immediately with a short “started” message.

**Completion handling**
- On subprocess exit:
  1) Read `.subtask/tasks/<task>/history.jsonl`, extract the most recent worker message event.
  2) Optionally load `.subtask/tasks/<task>/PROGRESS.json` and/or state for metadata.
  3) Call OpenCode `session.prompt_async` to send a **new user message** into the same session:
     - Include worker reply + “next action” framing (review → follow-up/stage/merge).
     - This automatically triggers OpenCode to respond, satisfying “no human intervention”.
  4) Also call `tui.show-toast` for a lightweight notification.

**Concurrency / safety**
- Track in-memory map `{task -> {sessionID, startedAt, pid}}` to avoid duplicate sends.
- If session is currently “busy”, either:
  - enqueue the injection until idle, or
  - inject with `noReply: true` + toast, then inject again when idle.
  (Pick one during implementation; simplest is queue + retry with backoff.)

### 3.2 Document installation paths

Support both:
- **Project-local plugin**: user copies a single file to `.opencode/plugin/subtask.ts`
- **npm plugin**: `opencode-subtask` published; user adds it to `opencode.json`:
  - `"plugin": ["opencode-subtask"]`

Project-local is easiest for initial rollout; npm is nicer long-term.

## 4) Changes Needed to Subtask (minimal + nice-to-haves)

### 4.1 Minimal required changes

None strictly required if the plugin:
- starts `subtask send` as a background process, and
- parses `.subtask/tasks/<task>/history.jsonl` for the latest worker message.

### 4.2 Recommended Subtask improvements (to make the plugin robust)

1) Add a machine-readable command:
   - `subtask show <task> --json` including:
     - status, stage, workspace path
     - last error
     - tool call counts / timings (if available)
     - latest worker reply (or a pointer to it)

2) Add `subtask conversation <task> --last-worker` (optionally `--json`) to avoid parsing raw text blocks.

3) Add `subtask send --quiet` to suppress the “Tip: Don’t check or poll…” banner when invoked by automation.

(These are not required for the MVP plugin but reduce fragility and parsing.)

## 5) OpenCode-Specific Skill Adjustments

Update the OpenCode skill file (if implementing this plan) to reflect OpenCode reality:

- Remove Claude-specific instruction: “Always use `run_in_background: true` for `send`”
- Replace with:
  - **Prereq:** “Install/enable the `opencode-subtask` plugin (project or global).”
  - **How to dispatch:** “Use the `subtask_send` tool (plugin) to start worker runs asynchronously.”
  - **Don’t poll:** “Do not loop on `subtask show`; the plugin will inject the worker reply into the chat when ready.”
  - **Continuation:** “When the worker reply arrives (plugin-injected message), respond normally: review → stage → follow-up/send → merge.”

Optional: keep a fallback section: “If the plugin isn’t installed, run `subtask send` manually and paste the worker reply.”

## 6) Implementation Checklist (for the next phase)

1) Subtask repo:
   - Add OpenCode plugin source (either vendored template under `plugin/` or a new `opencode/` folder) + docs.
   - Update the OpenCode skill file to use the plugin tool.
   - (Optional) add `subtask show --json` / `subtask conversation --last-worker`.

2) OpenCode plugin:
   - Implement background process spawn + completion handler.
   - Inject worker reply via `session.prompt_async`.
   - Add toast notification on completion.
   - Handle multiple concurrent tasks + session busy queueing.

3) Docs:
   - Add “OpenCode setup” section to Subtask README: plugin install + config examples.
