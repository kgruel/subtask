#!/bin/bash
# Stop hook: reminds the lead about unread worker replies before ending a turn.
#
# Runs `subtask unread` (exit 0 if any unread, exit 1 if none) in the project
# directory. If unread tasks exist, returns hookSpecificOutput.additionalContext
# nudging the lead to read them with `subtask reply <task>`.
#
# Non-blocking: returns additionalContext only, never decision: "block". The
# reminder is gentle. If `subtask` is not installed, or no .subtask/ exists in
# the cwd, the hook silently no-ops.
#
# stop_hook_active guard: per Claude Code docs, hooks must check this flag to
# avoid infinite re-trigger when they themselves cause the assistant to
# continue. We're returning additionalContext (not block), so re-triggering
# shouldn't happen — but the guard is cheap insurance.

set -u

INPUT=$(cat)

# Avoid infinite loops if Claude Code re-fires Stop after the assistant
# responds to our additionalContext.
ACTIVE=$(printf '%s' "${INPUT}" | jq -r '.stop_hook_active // false' 2>/dev/null)
if [ "${ACTIVE}" = "true" ]; then
  exit 0
fi

# Resolve cwd from the hook payload, falling back to the shell's cwd.
CWD=$(printf '%s' "${INPUT}" | jq -r '.cwd // empty' 2>/dev/null)
if [ -z "${CWD}" ] || [ ! -d "${CWD}" ]; then
  CWD="$(pwd)"
fi

# Walk up from cwd looking for a .subtask/ directory (matches preflightProject
# behavior: subtask state can live anywhere from cwd to git root).
PROJECT_DIR=""
DIR="${CWD}"
while [ "${DIR}" != "/" ] && [ -n "${DIR}" ]; do
  if [ -d "${DIR}/.subtask/tasks" ]; then
    PROJECT_DIR="${DIR}"
    break
  fi
  DIR="$(dirname "${DIR}")"
done

if [ -z "${PROJECT_DIR}" ]; then
  exit 0
fi

if ! command -v subtask >/dev/null 2>&1; then
  exit 0
fi

# `subtask unread` exits 0 with task names if any unread, exit 1 if none, and
# any other exit (e.g., 2 from "unknown command" on an older binary) means we
# can't trust the output — treat any non-zero as "nothing to report".
UNREAD=""
if pushd "${PROJECT_DIR}" >/dev/null 2>&1; then
  if OUTPUT=$(subtask unread 2>/dev/null); then
    # Defensive filter: keep only well-formed task names. Task names contain
    # alnum, slash, dash, underscore, dot — nothing else. Drops any stray
    # status text that escapes to stdout from subtask itself or its deps.
    UNREAD=$(printf '%s\n' "${OUTPUT}" | grep -E '^[A-Za-z0-9._/-]+$' || true)
  fi
  popd >/dev/null 2>&1 || true
fi

if [ -z "${UNREAD}" ]; then
  exit 0
fi

# Build additionalContext. Use jq to get safe JSON escaping.
COUNT=$(printf '%s\n' "${UNREAD}" | grep -c .)
if [ "${COUNT}" -eq 1 ]; then
  TASK=$(printf '%s' "${UNREAD}" | head -n1)
  MESSAGE="Unread worker reply on task '${TASK}'. Read it with: subtask reply ${TASK}"
else
  TASKS=$(printf '%s' "${UNREAD}" | tr '\n' ',' | sed 's/,$//; s/,/, /g')
  MESSAGE="Unread worker replies on tasks: ${TASKS}. Read each with: subtask reply <task>"
fi

if command -v jq >/dev/null 2>&1; then
  jq -nc --arg msg "${MESSAGE}" '{
    hookSpecificOutput: {
      hookEventName: "Stop",
      additionalContext: $msg
    }
  }'
else
  # Minimal fallback when jq is unavailable. Escape backslashes and quotes.
  ESCAPED=$(printf '%s' "${MESSAGE}" | sed 's/\\/\\\\/g; s/"/\\"/g')
  printf '{"hookSpecificOutput":{"hookEventName":"Stop","additionalContext":"%s"}}\n' "${ESCAPED}"
fi

exit 0
