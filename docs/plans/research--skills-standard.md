# Research: Skills standard for multi-agent support (Plan)

Source of truth for this task remains:
`.subtask/tasks/research--skills-standard/PLAN.md`

This file is a tracked copy so the plan is reviewable in git without relying on the runtime `.subtask/` directory (which is normally gitignored).

## Findings

**Note (2026-01-16):** Subtask now targets **Claude Code only** for lead skill distribution. The CLI manages the skill + plugin installation via `subtask install|uninstall|status`, targeting `~/.claude/skills/subtask/SKILL.md` (user scope) or `.claude/skills/subtask/SKILL.md` (project scope).

### 1) Anthropic “Agent Skills” open standard
- **Packaging:** A skill is a directory containing a `SKILL.md` file (with YAML frontmatter + Markdown body), optionally with additional resources (e.g. `references/`, `scripts/`). The goal is “progressive disclosure”: hosts can index metadata without loading the full body until needed.
- **Frontmatter:** `name` + `description` are required; the spec also defines optional keys such as `license`, `authors`, `tags`, `compatibility`, `dependencies`, and `allowed-tools` (noted as experimental).
- **Spec references (official):**
  - `https://agentskills.io/spec`
  - `https://github.com/anthropics/skills`
  - `https://github.com/openai/skills`
  - `https://www.anthropic.com/engineering/agent-skills`

### 2) Claude Code (Anthropic) skills + plugins
- **Skill discovery locations (project + user):**
  - `.claude/skills/<skill>/SKILL.md`
  - `~/.claude/skills/<skill>/SKILL.md`
  - Project skills override user skills.
- **Plugin packaging:** Claude Code plugins can bundle skills under `<plugin-root>/skills/<skill>/SKILL.md` with a plugin manifest at `<plugin-root>/.claude-plugin/plugin.json`.
- **Claude-specific frontmatter extensions:** Claude Code supports extra keys beyond the open spec (examples from docs): `allowed-tools`, `model`, `context` (e.g., `{type: fork}`), `agent`, `hooks`, and `user-invocable`.
- **Docs (official):**
  - Skills: `https://code.claude.com/docs/en/skills`
  - Plugins: `https://docs.claude.com/en/docs/claude-code/plugins`

### 3) Codex CLI (OpenAI) skills support
- **Skill discovery locations:** Codex uses its own skill discovery locations (omitted here). Subtask does not install lead skills for Codex.
- **Parsing/constraints (notable differences vs open spec):**
  - Requires `name` (≤ 100 chars) and `description` (≤ 500 chars).
  - Ignores extra YAML keys.
  - Ignores **symlinked** directories when scanning for skills.
  - Only injects `name`, `description`, and the file path by default; the full body is loaded when the skill is invoked.
- **Using skills:** Codex provides CLI helpers (e.g. `codex skills list`, `codex skills show <name>`) and supports explicitly requesting skills by name in chat.
- **Docs (official):**
  - Overview: `https://developers.openai.com/codex/skills`
  - Create: `https://developers.openai.com/codex/skills/create-custom-skills`
  - Use: `https://developers.openai.com/codex/skills/using-skills`

### 4) OpenCode skills support
- **Skill discovery locations:** OpenCode uses its own skill discovery locations (omitted here). Subtask does not install lead skills for OpenCode.
- **Parsing:** Requires `name` + `description` and ignores other frontmatter keys.
- **Docs (official):**
  - `https://opencode.ai/docs/skills`

## Differences (what matters for multi-agent support)
- **Unified file format:** All three can consume the Agent Skills-style `SKILL.md` (YAML frontmatter + Markdown body), but **support different optional keys**.
- **Discovery paths differ** across tools (Subtask targets Claude Code only for lead skill distribution).
- **Strictness differs:**
  - Codex enforces `description ≤ 500` and ignores symlinked skill directories.
  - OpenCode recognizes only `name`/`description` (ignores the rest).
  - Claude Code supports the richest set of extensions (tools/model/hooks/subagents).
- **Async/background conventions are agent-specific:** Claude Code has a documented “run in background” flow; other agents don’t share an identical mechanism, but the *behavioral requirement* for `subtask` is consistent: `send` dispatches work asynchronously and the lead should stop and wait.

## Subtask skill distribution (Claude Code only)

- Subtask’s canonical skill source is embedded in the CLI at `pkg/install/SKILL.md`.
- Install/uninstall/status is managed explicitly with:
  - `subtask install`
  - `subtask uninstall`
  - `subtask status`
- The install targets are:
  - User: `~/.claude/skills/subtask/SKILL.md` and `~/.claude/plugins/subtask/`
  - Project: `.claude/skills/subtask/SKILL.md` and `.claude/plugins/subtask/`

### Handling async/background differences inside `SKILL.md`

Include a single, agent-neutral rule plus agent-specific implementation notes:
- **Invariant:** `subtask send` starts/resumes work asynchronously; after issuing it, **stop** and tell the user you’re waiting for the worker response (don’t poll).
- **Claude Code:** use the documented background execution mechanism when running `subtask send`, then stop.
- **Other agents:** run `subtask send` normally (it should return quickly), then stop; don’t loop on `subtask show`.
