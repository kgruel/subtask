# Subtask ✨

Subtask lets Claude Code draft your tasks, spawn subagents, track progress, review code and request changes.

Codex subagents are supported and recommended.

### Spawning Tasks
```makefile
You: fix the auth bug and add API metrics with Subtask

Claude:
  ├─► Bash(subtask draft fix/auth-bug ...)
  ├─► Bash(subtask draft feat/api-metrics ...)
  └─⏺ Tasks are in progress! I'll get notified when they're done.
```

### Tracking & Merging
```makefile
You: which tasks are open?

Claude:
  ├─► Bash(subtask list)
  │   ┌──────────────────┬─────────┬────────────────────────┐
  │   │ TASK             │ STATUS  │ TITLE                  │
  │   ├──────────────────┼─────────┼────────────────────────┤
  │   │ fix/auth-bug     │ replied │ Fix auth token refresh │
  │   │ feat/api-metrics │ working │ Add API metrics        │
  │   │ feat/ui-redesign │ draft   │ Redesign dashboard     │
  │   └──────────────────┴─────────┴────────────────────────┘
  │
  └─⏺ ui-redesign hasn't started yet — want me to kick it off?

You: lets merge fix/auth-bug.
```

## Install

### Get the CLI

#### Mac/Linux

```bash
curl -fsSL https://subtask.dev/install.sh | bash
```

#### Windows (PowerShell)

```powershell
irm https://subtask.dev/install.ps1 | iex
```

<details>
<summary>Other install methods…</summary>

#### Homebrew

```bash
brew install zippoxer/tap/subtask
```

#### Go

```bash
go install github.com/zippoxer/subtask/cmd/subtask@latest
```

#### Binary

[GitHub Releases](https://github.com/zippoxer/subtask/releases)

</details>

### Install the Claude Code Skill

```bash
subtask install
```

Restart Claude Code.

### Setup Subtask in your Repo

In Claude Code, run `/subtask:setup`.

*Tip: You can set it up manually with `subtask init`.*

## Use

Ask Claude Code to do things:

- "fix the login bug with Subtask"
- "run these 3 features in parallel"
- "plan and implement the new API endpoint with Subtask"

Claude Code will draft tasks and run them simultaneously in isolated Git worktrees, then help you review and merge the changes.

## TUI
<img width="850" height="521" alt="image (2)" src="https://github.com/user-attachments/assets/fcc4686a-afa1-4168-b141-e54d9286ad4c" />
<img width="850" height="670" alt="image (1)" src="https://github.com/user-attachments/assets/f4954222-62cc-40ed-8a26-1797af4d206d" />

## Updating
```bash
subtask update --check
subtask update
```

## License

MIT
