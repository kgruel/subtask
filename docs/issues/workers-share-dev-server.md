# Workers Share Dev Server Instead of Running Own

## Summary

Workers testing web UIs connect to the lead's dev server, not their own. Code changes in worker worktrees aren't reflected in what agent-browser tests.

## Problem

When a worker uses `agent-browser` to test `localhost:3000`:
1. The lead's Vite dev server is running on port 3000 (from lead's workspace)
2. Worker makes code changes in its isolated worktree
3. Worker tests against lead's server → testing **unchanged code**
4. Worker thinks fix works, but it's actually untested

## Discovery

During `web/fix-homepage` task:
- Lead's Vite: `/Users/zippo/code/finality/web/` on port 3000
- Worker's worktree: `/Users/zippo/.subtask/workspaces/-Users-zippo-code-finality--2/web/`
- Only one Vite process running (lead's)
- Worker's +24 lines of changes never served to browser

## Root Cause

1. Workers don't automatically start their own dev servers
2. Port 3000 already bound by lead's dev stack
3. No WORKSPACE_ID propagation to workers for port offsetting
4. Workers assume `localhost:3000` works without checking who's serving it

## Impact

- False positives: Worker reports fix works when it doesn't
- Wasted iteration: Lead merges, discovers bug still exists
- Confusion: "It worked in the worker's testing"

## Potential Solutions

### Option A: Worker-specific ports
- Workers use WORKSPACE_ID to offset all ports
- Worker 1: Vite on 3001, CH on 8124, PG on 5433
- Requires dev stack to support parallel instances fully

### Option B: Workers start own Vite only
- Keep shared CH/PG/Kurtosis (data layer)
- Each worker starts Vite in its worktree on unique port
- agent-browser told which port to use

### Option C: Explicit handoff
- Lead stops dev server before worker tests
- Worker starts its own, tests, stops
- Lead resumes
- Con: Sequential, slow

### Option D: Documentation/workflow
- Document that workers should verify their Vite is running
- Add check: "is Vite serving from my worktree?"
- Workers explicitly start `npm run dev -- --port 300X`

## Recommendation

Option B seems most practical:
- Shared backend (Kurtosis/CH/PG) is fine - workers need same data
- Each worker runs own Vite on offset port
- `subtask send` could auto-assign VITE_PORT=3000+worker_id
- agent-browser uses that port

## Related

- `docs/issues/follow-up-base-branch-ux.md` - another subtask UX issue from same session
