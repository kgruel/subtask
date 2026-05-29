#!/bin/bash
# UserPromptSubmit hook: warns the lead about stale running workers.
#
# Detects tasks whose worker has been running with no history activity for
# longer than SUBTASK_STALE_THRESHOLD_MIN (default 30 minutes).
#
# "Stale" is defined as: task history.jsonl shows a worker.started whose
# run_id has no corresponding worker.finished, AND the file has not been
# modified in the threshold interval (no tool calls or events appended).
#
# Emits hookSpecificOutput.additionalContext listing stale tasks. Never
# blocks (no decision: "block"). Silent no-op if subtask is not installed,
# no .subtask/ in the tree, or no stale workers.

set -u

THRESHOLD_MIN="${SUBTASK_STALE_THRESHOLD_MIN:-30}"

INPUT=$(cat)

# Resolve cwd from the hook payload, falling back to the shell's cwd.
CWD=$(printf '%s' "${INPUT}" | jq -r '.cwd // empty' 2>/dev/null)
if [ -z "${CWD}" ] || [ ! -d "${CWD}" ]; then
  CWD="$(pwd)"
fi

# Walk up from cwd looking for a .subtask/tasks directory.
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

if ! command -v jq >/dev/null 2>&1; then
  exit 0
fi

NOW=$(date +%s)
THRESHOLD_SEC=$(( THRESHOLD_MIN * 60 ))

# Portable mtime in epoch seconds.
#
# GNU form (--format) MUST come first: on Linux `stat -f` means --file-system
# and *succeeds* (exit 0) printing garbage that starts with "File:", so the
# BSD-first ordering never falls through and MTIME ends up non-numeric —
# breaking the later $(( NOW - MTIME )) arithmetic ("File: unbound variable"
# under set -u). The GNU long option fails cleanly on macOS BSD stat, so
# GNU-first is correct on both platforms.
file_mtime() {
  stat --format='%Y' "$1" 2>/dev/null || stat -f '%m' "$1" 2>/dev/null || echo 0
}

# Convert an ISO8601 timestamp (from history.jsonl) to epoch seconds.
# Strips sub-second precision and treats as UTC. Returns 0 on parse failure.
iso_to_epoch() {
  local base
  base=$(printf '%s' "$1" | sed 's/\.[0-9]*//; s/Z$//')
  date -j -f '%Y-%m-%dT%H:%M:%S' "${base}" '+%s' 2>/dev/null \
    || date -d "${base}Z" '+%s' 2>/dev/null \
    || echo 0
}

STALE_LINES=""

for TASK_DIR in "${PROJECT_DIR}/.subtask/tasks"/*/; do
  [ -d "${TASK_DIR}" ] || continue
  HISTORY="${TASK_DIR}history.jsonl"
  [ -f "${HISTORY}" ] || continue

  # Quick mtime check first — skip if recently active.
  MTIME=$(file_mtime "${HISTORY}")
  AGE_SEC=$(( NOW - MTIME ))
  if [ "${AGE_SEC}" -lt "${THRESHOLD_SEC}" ]; then
    continue
  fi

  # Collect run_ids of all completed runs.
  FINISHED_IDS=$(jq -r 'select(.type == "worker.finished") | (.data.run_id // "")' "${HISTORY}" 2>/dev/null | grep -v '^$' | sort -u || true)

  # Get last worker.started: "<run_id>\t<ts>"
  LAST_STARTED=$(jq -r 'select(.type == "worker.started") | "\(.data.run_id // "")\t\(.ts // "")"' "${HISTORY}" 2>/dev/null | tail -n1 || true)
  [ -z "${LAST_STARTED}" ] && continue

  STARTED_RUN_ID=$(printf '%s' "${LAST_STARTED}" | cut -f1)
  RUNNING_TS=$(printf '%s' "${LAST_STARTED}" | cut -f2)
  [ -z "${RUNNING_TS}" ] && continue

  # Skip if this run already finished.
  if [ -n "${FINISHED_IDS}" ] && printf '%s\n' "${FINISHED_IDS}" | grep -qF "${STARTED_RUN_ID}"; then
    continue
  fi

  # Convert started timestamp to epoch.
  STARTED_EPOCH=$(iso_to_epoch "${RUNNING_TS}")
  [ "${STARTED_EPOCH}" -eq 0 ] && continue

  RUNNING_MIN=$(( (NOW - STARTED_EPOCH) / 60 ))
  IDLE_MIN=$(( AGE_SEC / 60 ))

  # Unescape task name: "fix--bug" → "fix/bug"
  DIR_NAME=$(basename "${TASK_DIR%/}")
  TASK_NAME=$(printf '%s' "${DIR_NAME}" | sed 's/--/\//g')

  LINE="${TASK_NAME}: running ${RUNNING_MIN}m, last activity ${IDLE_MIN}m ago — subtask trace ${TASK_NAME} or subtask interrupt ${TASK_NAME}"
  if [ -z "${STALE_LINES}" ]; then
    STALE_LINES="${LINE}"
  else
    STALE_LINES="${STALE_LINES}
${LINE}"
  fi
done

if [ -z "${STALE_LINES}" ]; then
  exit 0
fi

COUNT=$(printf '%s\n' "${STALE_LINES}" | grep -c '.')
if [ "${COUNT}" -eq 1 ]; then
  HEADER="Stale worker (no activity ${THRESHOLD_MIN}+ min):"
else
  HEADER="Stale workers (no activity ${THRESHOLD_MIN}+ min each):"
fi
MESSAGE="${HEADER}
${STALE_LINES}"

jq -nc --arg msg "${MESSAGE}" '{
  hookSpecificOutput: {
    hookEventName: "UserPromptSubmit",
    additionalContext: $msg
  }
}'

exit 0
