# Add Claude Code CLI as a Harness (Plan)

Source of truth for this task remains:
`.subtask/tasks/feat--claude-code-harness/PLAN.md`

This file is a tracked copy so the plan is reviewable in git without relying on the runtime `.subtask/` directory (which is normally gitignored).

Update (2026-01-13): cross-workspace resume is technically possible by copying the Claude session files into the destination project folder, but it is unsafe unless the copied session log is rewritten to replace the old workspace root path with the new workspace root path (otherwise Claude may read/write files in the original workspace). This should be treated as an experimental/opt-in approach.
