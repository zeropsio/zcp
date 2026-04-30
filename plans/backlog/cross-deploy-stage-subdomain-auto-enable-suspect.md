# SUSPECT — subdomain auto-enable on cross-deploy stage half may not fire

**Surfaced**: 2026-04-30 — Tier-3 eval `bootstrap-classic-node-standard` (suite `20260430t154900-9c00ab`). Agent reported `Outcome: SUCCESS` and that `GET /todos returns 200 + valid JSON array on both` (appdev + appstage). Probe at grade time still failed for the stage half:

```
finalUrlStatus: probe of  failed: service "appstage" has no reachable subdomain URL (enable subdomain first)
```

Two readings of this:

1. Agent verified appstage via `ssh appstage "curl localhost:<port>/todos"` (not the subdomain URL) — the `200 + JSON` claim is real but the subdomain was never actually enabled. The probe then correctly reports it can't reach.
2. Subdomain auto-enable DID fire but the probe runs too quickly after deploy completion to catch the L7 propagation window — flake.

If reading (1) is right: this is a real handler/atom bug — spec O3 says auto-enable fires on first deploy for eligible modes (dev/stage/simple/standard/local-stage). For standard mode this should include the stage half on its first cross-deploy. Either the auto-enable predicate is missing the cross-deploy stage path, or the atom guidance doesn't tell agents to call `zerops_subdomain action="enable"` for stage and they skip it.

**Why deferred — and labeled SUSPECT**: needs investigation before committing fix direction. The signal is one scenario, ambiguous read between handler bug and probe-timing flake. Bisecting:
- Read `internal/tools/deploy_subdomain.go` auto-enable predicate — does it cover the cross-deploy `targetService=stage` case?
- Check Tier-1 standard-mode scenarios that PASSed (weather-dashboard-bun, weather-dashboard-php-laravel) — did THEY have explicit subdomain enables in their tool-calls.json? If yes, auto-enable wasn't firing for them either; if no, it works for those but broke for S11 specifically.
- If auto-enable fires correctly: adjust scenario probe to wait + retry once for L7 propagation.

**Trigger to promote**: investigation confirms reading (1) — handler-side bug or atom-guidance gap. Then fix lives wherever the predicate / atom is. If reading (2) — probe flake — fix is in `internal/eval/probe.go` (retry/backoff once on initial fail), not in core handlers.

## Sketch

Investigation order (~30 min):

1. Read `internal/tools/deploy_subdomain.go` `IsAutoEnableEligible` (or whatever the predicate is named) — confirm it accepts standard-stage cross-deploys.
2. Walk Tier-1 weather-dashboard-bun + weather-dashboard-php-laravel `tool-calls.json` — count `zerops_subdomain action="enable"` calls explicitly made by the agent. Compare to S11.
3. Check `zerops_events` for the S11 suite specifically — was there a `subdomain-enable` process event on appstage between cross-deploy and grade time?
4. Conclude: handler bug, atom gap, or probe flake.

Each branch then has a clear fix.

## Risks

- "Suspect" entries clutter the backlog if they linger unread. If investigation isn't done within the next 1-2 phases of eval work, either close out as "couldn't reproduce" or promote to a real plan.
- If the agent did test via SSH-localhost not subdomain, the SUCCESS in the EVAL REPORT is misleading — agent didn't verify what users would see. That's a separate issue worth surfacing.

## Refs

- Tier-3 triage: `/Users/macbook/Documents/Zerops-MCP-evals/2026-04-30/TIER3-TRIAGE.md` §S11
- Per-scenario log + tool-calls: `tier3/bootstrap-classic-node-standard/{log.jsonl,tool-calls.json}`
- Spec O3: `docs/spec-workflows.md` — auto-enable on first deploy for eligible modes
- Prior commit: `6a246201 fix(subdomain): unify auto-enable eligibility predicate (HTTP-signal-aware)` — recent change in this area
