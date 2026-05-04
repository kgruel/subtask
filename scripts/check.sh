#!/usr/bin/env bash
# Mirror the GitHub Actions CI checks locally so a task can be verified
# before pushing. Replicates .github/workflows/ci.yml:
#
#   1. gofmt -l .   (must produce no output)
#   2. go vet ./...
#   3. go test ./...
#
# Windows-specific failures in CI cannot be reproduced on macOS/Linux and
# are skipped here intentionally — they're environment quirks (git
# `$GIT_DIR too big`, path escaping), not portable bugs.
#
# Usage:
#   scripts/check.sh           # run all checks; exit nonzero on first failure
#   scripts/check.sh --fix     # run `gofmt -w` to auto-format, then run checks
#   scripts/check.sh --short   # run `go test -short ./...` (skip e2e)

set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

FIX=0
TEST_FLAGS=()
for arg in "$@"; do
  case "$arg" in
    --fix)   FIX=1 ;;
    --short) TEST_FLAGS+=(-short) ;;
    -h|--help)
      sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "unknown flag: $arg (try --help)" >&2
      exit 2
      ;;
  esac
done

fail=0
section() { printf '\n=== %s ===\n' "$1"; }

section "gofmt"
if [[ $FIX -eq 1 ]]; then
  gofmt -w .
  echo "gofmt -w applied"
fi
fmt_files="$(gofmt -l .)"
if [[ -n "$fmt_files" ]]; then
  echo "gofmt needed on:"
  echo "$fmt_files"
  echo "(run 'scripts/check.sh --fix' to auto-format)"
  fail=1
else
  echo "ok"
fi

section "go vet"
if ! go vet ./...; then
  fail=1
fi

section "go test ${TEST_FLAGS[*]:-}"
if ! go test "${TEST_FLAGS[@]}" ./...; then
  fail=1
fi

section "summary"
if [[ $fail -eq 0 ]]; then
  echo "all checks passed"
  exit 0
fi
echo "one or more checks failed (see above)"
exit 1
