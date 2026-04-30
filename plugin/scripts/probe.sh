#!/bin/bash
# Probe hook: logs the JSON payload of each hook invocation to ~/.subtask/probe.jsonl,
# annotated with the wrapper-provided event name and a timestamp.
#
# Gated on env var SUBTASK_PROBE. When unset, exits immediately with no logging.
# Set SUBTASK_PROBE=1 in the environment before launching Claude Code to enable.
#
# Wired into plugin/hooks/hooks.json for the events we want to verify. The first
# positional argument MUST be the event name (e.g., PostToolUse, UserPromptSubmit,
# Stop, Notification, PreToolUse).
#
# This script never emits hookSpecificOutput — it's diagnostic only and must not
# affect the assistant's behavior. Always exits 0.

set -u

if [ -z "${SUBTASK_PROBE:-}" ]; then
  exit 0
fi

EVENT_NAME="${1:-unknown}"
LOG_DIR="${HOME}/.subtask"
LOG_FILE="${LOG_DIR}/probe.jsonl"

mkdir -p "${LOG_DIR}" 2>/dev/null || exit 0

INPUT=$(cat)
TS=$(date -u +"%Y-%m-%dT%H:%M:%S.%6NZ" 2>/dev/null || date -u +"%Y-%m-%dT%H:%M:%SZ")

# Wrap the raw hook input with our envelope. Use jq so we get a single valid JSON
# line; if jq is unavailable, fall back to a minimal envelope so we at least see
# the event firing.
if command -v jq >/dev/null 2>&1; then
  printf '%s\n' "${INPUT}" | jq -c \
    --arg ts "${TS}" \
    --arg event "${EVENT_NAME}" \
    '{ts: $ts, event: $event, payload: .}' \
    >> "${LOG_FILE}" 2>/dev/null
else
  printf '{"ts":"%s","event":"%s","payload_raw":%s}\n' \
    "${TS}" "${EVENT_NAME}" \
    "$(printf '%s' "${INPUT}" | sed 's/\\/\\\\/g; s/"/\\"/g')" \
    >> "${LOG_FILE}" 2>/dev/null
fi

exit 0
