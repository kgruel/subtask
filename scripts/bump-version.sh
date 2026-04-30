#!/usr/bin/env bash
# Bump the subtask version in lockstep across:
#   - cmd/subtask/main.go      (binary fallback version)
#   - .claude-plugin/marketplace.json (plugin entry version)
#   - plugin/.claude-plugin/plugin.json (plugin manifest version)
#
# The contract is strict: binary version == plugin version, always. Users
# installing the binary at version X expect a plugin from the same commit,
# and Claude Code shows the plugin version in its UI.
#
# Usage: scripts/bump-version.sh <new-version>
#   new-version is bare semver (no leading 'v'), e.g. 0.4.0

set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <new-version>" >&2
  echo "  e.g. $0 0.4.0" >&2
  exit 2
fi

NEW="$1"
if ! [[ "$NEW" =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][A-Za-z0-9.+-]+)?$ ]]; then
  echo "error: '$NEW' is not a valid semver version (expected e.g. 0.4.0)" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

MAIN_GO="cmd/subtask/main.go"
MARKETPLACE=".claude-plugin/marketplace.json"
PLUGIN="plugin/.claude-plugin/plugin.json"

for f in "$MAIN_GO" "$MARKETPLACE" "$PLUGIN"; do
  [[ -f "$f" ]] || { echo "error: missing $f" >&2; exit 1; }
done

CURRENT="$(grep -E '^[[:space:]]*version[[:space:]]*=[[:space:]]*"[^"]+"' "$MAIN_GO" | head -1 | sed -E 's/.*"([^"]+)".*/\1/')"
echo "current binary version: $CURRENT"
echo "new version:            $NEW"

# main.go: version = "X.Y.Z"
perl -i -pe 's/(version\s*=\s*")[^"]+(")/${1}'"$NEW"'$2/' "$MAIN_GO"

# Update "version": "..." in plugin manifests. There is one such line per
# file today; if a future schema adds another, this will update both — review
# the diff before committing.
perl -i -pe 's/("version":\s*")[^"]+(")/${1}'"$NEW"'$2/' "$MARKETPLACE"
perl -i -pe 's/("version":\s*")[^"]+(")/${1}'"$NEW"'$2/' "$PLUGIN"

echo
echo "Updated:"
git --no-pager diff --stat "$MAIN_GO" "$MARKETPLACE" "$PLUGIN" || true
echo
echo "Next steps:"
echo "  1. Review the diff: git diff $MAIN_GO $MARKETPLACE $PLUGIN"
echo "  2. Commit:         git commit -am \"Bump version to $NEW\""
echo "  3. Tag and push:   git tag v$NEW && git push fork main && git push fork v$NEW"
