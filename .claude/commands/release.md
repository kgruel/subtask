---
allowed-tools: Bash(git:*), Bash(gh:*)
argument-hint: major | minor | patch
description: Create and publish a new release
---

## Context

- Current version: !`git describe --tags --abbrev=0 2>/dev/null || echo "no tags yet"`
- Current branch: !`git branch --show-current`
- Git status: !`git status --short`
- Commits since last tag: !`git log $(git describe --tags --abbrev=0 2>/dev/null)..HEAD --oneline 2>/dev/null || git log --oneline -5`

## Task

Create a new **$1** release (major | minor | patch).

### Steps

1. **Validate** argument is one of: major, minor, patch. If missing or invalid, ask.

2. **Check prerequisites**:
   - On `main` branch
   - Working directory is clean
   - Tests pass: `go test ./...`

3. **Calculate new version** using semver:
   - major: bump X in vX.Y.Z, reset Y and Z to 0
   - minor: bump Y, reset Z to 0
   - patch: bump Z

4. **Create and push tag**:
   ```bash
   VERSION=vX.Y.Z
   git tag "$VERSION"
   git push origin "$VERSION"
   ```

5. **Monitor release workflow**:
   ```bash
   gh run watch --workflow release.yml --interval 10
   ```

6. **Verify release**:
   ```bash
   gh release view "$VERSION"
   gh release view "$VERSION" --json assets --jq '.assets[].name'
   ```

7. **Add release notes**:
   - Read the commit history since the last release
   - Group changes by type (Features, Fixes, Improvements, etc.)
   - Write a concise summary highlighting the most important changes
   - Update the release notes:
   ```bash
   gh release edit "$VERSION" --notes "$(cat <<'EOF'
   ## What's New

   ### Features
   - Feature 1
   - Feature 2

   ### Fixes
   - Fix 1
   - Fix 2

   ### Improvements
   - Improvement 1

   **Full Changelog**: https://github.com/zippoxer/subtask/compare/vPREVIOUS...$VERSION
   EOF
   )"
   ```

8. **Verify Homebrew tap updated**:
   ```bash
   gh api "repos/zippoxer/homebrew-tap/contents/Formula/subtask.rb?ref=main" --jq .content \
     | base64 --decode \
     | rg "version|url|sha256" -n
   ```

9. **Test Homebrew install** (without installing):
   ```bash
   brew fetch --force zippoxer/tap/subtask
   ```
   This downloads the tarball and verifies the checksum matches the formula.

Note: Do NOT update the local installation. The user tests with local builds (`go install ./cmd/subtask`), not Homebrew.

### Troubleshooting

If release workflow fails:
```bash
gh run list --workflow release.yml --limit 5
gh run view --log-failed <run-id>
```
Fix the issue on main, then create a NEW tag (don't reuse).

To undo a bad release:
```bash
git tag -d "$VERSION"
git push origin ":refs/tags/$VERSION"
gh release delete "$VERSION" -y
```
