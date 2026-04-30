# "Set close-mode for auto-close to fire" is implicit, not surfaced upfront

**Surfaced**: 2026-04-30 — Tier-3 eval `bootstrap-classic-node-standard` (suite `20260430t154900-9c00ab`). Agent's EVAL REPORT (Information gap):

> The develop workflow guidance mentions `auto-close gated by close-mode` in every deploy response, but the initial develop-active guidance doesn't explicitly state that I need to set close-mode before auto-close can fire. I figured it out from the `workSessionState.reason` field in deploy responses. The knowledge base SHOULD have a prominent note in the develop first-deploy guidance saying "set close-mode on all in-scope services before expecting auto-close" with the exact tool call.

**Why deferred**: minor UX paper-cut — agent recovered from `workSessionState.reason` in deploy responses without explicit guidance; scenario still PASSed at agent level. The fix is one atom edit but worth doing in a batch with other develop-atom guidance updates rather than a one-line drive-by commit. Phase 1.5 (develop-flow iteration scenarios) will surface more first-deploy / close-mode interplay; bundle this with whatever else those evals find.

**Trigger to promote**: a Phase 1.5 or later eval where the agent waits for auto-close without setting close-mode and visibly stalls (timeout, max-turns) instead of recovering via workSessionState.reason hint.

## Sketch

Single atom edit in `internal/content/atoms/develop-first-deploy-intro.md` — add a "**Auto-close prerequisite**" callout near the verify+close steps:

```markdown
### Auto-close prerequisite

After the first deploy lands and verifies, auto-close fires only when
every in-scope service has a confirmed `closeDeployMode`. If you want
the session to close itself rather than calling `action="close"`
explicitly, set the close-mode now:

zerops_workflow action="close-mode" closeMode={"<host>":"auto"}

The deploy response carries `workSessionState.reason` — that field
tells you whether auto-close is gated on a missing close-mode or
something else (failed verify, scope mismatch).
```

Alternatively / additionally: `develop-auto-close-semantics.md` could also surface this if it doesn't already. Verify scope before editing.

## Risks

- The visible-deploy-response field is `workSessionState.reason` — the atom must use the same field name to avoid drift. Verify against `internal/tools/deploy_record.go` or wherever the response is composed.
- Two atoms talking about auto-close gating risks duplication; pick one canonical home.

## Refs

- Tier-3 triage: `/Users/macbook/Documents/Zerops-MCP-evals/2026-04-30/TIER3-TRIAGE.md` §S11 Information gaps
- Per-scenario assessment: `tier3/bootstrap-classic-node-standard/result.json`
- Existing atoms: `develop-first-deploy-intro.md`, `develop-auto-close-semantics.md`
- Field source: `workSessionState.reason` populated by deploy/verify responses (introduced in Round 2 fixes — `fix(verify): extend WorkSessionState lifecycle signal to verify response` + `fix(deploy-response): structured WorkSessionState lifecycle signal`)
