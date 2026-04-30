---
slug: record-deploy-build-id-correlation
---

# `record-deploy` build-ID correlation in the build-status gate

**Surfaced**: 2026-04-30 (round-3 pre-internal-testing audit, F1 structural follow-up). Round-3A fix (`workflow_record_deploy.go::recordDeployBuildStatusGate` + `internal/ops/events.go` startWithoutCode filter) closes the two narrow holes — the gate now runs on every record-deploy and never accepts a `startWithoutCode` ACTIVE — but does NOT solve the broader correlation question: the gate still picks the **most recent** deploy/build event without binding to the specific build the agent's last push triggered.

**Why deferred**: Round-3A's narrower fix is enough for the smoke-test path. The remaining hole — agent calls record-deploy mid-iteration before their just-pushed build event has overtaken a prior ACTIVE in the timeline — is a race window measured in seconds, gated by the agent's own polling discipline (the `develop-build-observe` atom tells them to wait for `Status=ACTIVE` first). The structural fix changes the contract — either record-deploy gains a required `buildId` arg or `ServiceMeta` gains a `LastPushAt` cache — both of which are big enough to need their own design pass rather than getting wedged into a "small fix" commit.

**Trigger to promote**: any of the following:
1. Live-agent eval surfaces a record-deploy that acked a stale ACTIVE (verify failed against old code; auto-close fired against unfinished build). The matrix simulator can't reproduce this — it needs real platform timing.
2. A second class of "wrong build acked" bug that would also be solved by build-ID correlation lands in the audit pipeline.
3. We add a new feature that requires the agent to track which specific appVersion an action targeted (e.g. per-build verify, build-specific rollback) — that work shares the same correlation primitive.

## Sketch

Two viable contract designs, mutually exclusive:

### Design A — explicit `buildId` arg on `record-deploy`

Agent flow:
```
zerops_deploy strategy="git-push" targetService="appdev"
zerops_events serviceHostname="appdev"   → finds latest BUILDING/ACTIVE event for the new push
                                           → reads its appVersion ID (e.g. "av-abc123")
zerops_workflow action="record-deploy" targetService="appdev" buildId="av-abc123"
```

Handler validates `buildId` matches the latest event AND that event's status is ACTIVE. Refuses otherwise.

**Pros**: contract is explicit; agent KNOWS which build they're acking; no ambiguity.
**Cons**: agent burden (must read events first AND extract ID); breaks current contract; needs atom rewrite (`develop-build-observe`, `develop-record-external-deploy`); needs eval scenario updates.

### Design B — `ServiceMeta.LastPushAt` cache + per-push event filter

`zerops_deploy strategy="git-push"` stamps `meta.LastPushAt = time.Now()` after the push transmits.

`recordDeployBuildStatusGate` then filters events to those with `Timestamp > meta.LastPushAt` before picking the latest. If no event matches the filter, refuse with "no build event seen since your last push at <LastPushAt> — wait for it".

**Pros**: agent contract unchanged (no buildId arg); works automatically; cleanest agent UX.
**Cons**: Push-transmit time ≠ build-event-creation time — race window between stamp and platform recording the event (small but non-zero). Adds a meta field. Doesn't help if push happened OUTSIDE ZCP (webhook from external git push, GitHub Actions zcli push from CI) — in those cases LastPushAt is stale and the gate would refuse a legitimate ack.

### Recommendation

Design B for ZCP-initiated git-push (covers ~90% of the lifecycle); Design A as a manual-override for the "I pushed externally" case (`record-deploy targetService=X buildId=av-abc123` skips the timestamp filter when buildId is present).

## Risks

- **Time-skew between agent and platform**: if container clock drifts vs platform timestamp, Design B's filter misses events. Mitigation: stamp from platform-side timestamp, not local time.
- **External-CI race**: external GitHub Actions calls `zcli push` from CI; ZCP doesn't see the push event. LastPushAt stays stale; Design B refuses legitimate ack. Mitigation: Design A escape hatch.
- **Atom corpus rewrite cost**: any contract change needs `develop-build-observe`, `develop-record-external-deploy`, `setup-build-integration-{webhook,actions}` re-authoring + matrix scenarios.

## Refs

- `docs/audit-prerelease-internal-testing-2026-04-30-roundthree.md` F1
- `internal/tools/workflow_record_deploy.go::recordDeployBuildStatusGate`
- `internal/ops/progress.go::isStartWithoutCodeEvent` (the round-3A surgical fix mirrors this filter pattern in events.go)
- `internal/content/atoms/develop-build-observe.md`
- `internal/content/atoms/develop-record-external-deploy.md`
