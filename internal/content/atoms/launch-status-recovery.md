---
id: launch-status-recovery
priority: 4
phases: [launch-production-active]
title: "Launch status — mid-flight recovery"
references-fields: []
---

### Launch status — recovery

`action="status"` surfaces one of three launch-production envelope shapes when a state file exists for this source project. Conversation context was likely lost (compaction, restart) — the envelope carries enough state to resume or terminate cleanly.

#### `kind: "launch-active"` — mid-flight

A non-terminal launch is in progress. Resume via `action="start"` with the same `productionProjectName`.

| Field | Use |
|---|---|
| `targetProjectName` | Pass back as `productionProjectName` on the resume call. |
| `status` | Phase to expect next response (`ready-to-launch` advertises `delegatedLaunch.available` — advance with `confirmLaunch=true` when `true`, `launchKey` as the fallback; `launching` / `configuring-pipeline` are polling). |
| `lastUpdate` | Sanity-check freshness — minutes old = active; days old = user may have abandoned (ask before resuming). |
| `ambiguousChoices` | Multiple non-terminal launches exist; pick a `productionProjectName` before resume. |

Resume call shape:

```
zerops_workflow action="start" workflow="launch-production" productionProjectName="<from envelope>"
```

`action="start"` is required on every call — launch-production is stateless multi-call narrowing, `action="start"` is the only orchestration entry (no classify action, no `action="complete"`). The handler re-reads accumulated state from `productionProjectName` and advances to the next phase.

The `launchKey` is NOT required at the status step. When the workflow re-enters `ready-to-launch` and you intend to advance to `launching`, check `delegatedLaunch.available` first — if a platform delegation is available, re-call with `confirmLaunch=true` instead. The fallback when no delegation is available: ask the user to generate a launch token in the dashboard and pass it as `launchKey` — never create or guess a token value yourself. Status is read-only; ZCP never constructs a project-admin client on this path.

#### `kind: "launch-failed"` — terminal failure; recovery depends on `targetProjectId`

The most-recent launch for this source project ended in `failed` (e.g. schema validation rejected the import, mutation API error). The envelope's `nextCall` carries the correct recovery — the split:

- **`targetProjectId` EMPTY — retry directly.** No production project was created; re-call `action="start"` with the same `productionProjectName`. When `tokenAcquisition` is `"delegated"`, retry with `confirmLaunch=true`: with `targetServiceHostname` present the prior attempt staged the token and the retry reuses it (no new delegation needed or consumed); without it the retry re-checks delegation availability and otherwise falls back to the manual path. Do NOT reset here unless you intend to ABANDON the launch: reset deletes any staged token, the one-time delegation is already spent, and the manual dashboard walkthrough becomes the only remaining path. `mintedTokenName` names the standing token visible in the dashboard.
- **`targetProjectId` PRESENT — reset required.** A partial production project exists; a blind retry would duplicate services or collide on project envs. Clear it first:

```
zerops_workflow action="reset" workflow="launch-production" productionProjectName="<from envelope>"
```

`action="reset"` is destructive — the first call returns a `wouldDestroy` diagnostic listing what will be removed; the second call must echo it back as a structured `confirmDestructive={"operation":"launch-production-reset","acknowledgedTargets":[...]}` ack (matching the `wouldDestroy` payload) to clear the gate. The orphan production project is deleted too when the launch token resolves — from the staged `ZCP_LAUNCH_TOKEN` secret on the source push service (no re-send needed), or via an explicit `launchKey` when the staged secret is gone. Without any token, reset only clears local state and leaves the billable orphan for manual dashboard deletion. (Old binaries without reset support: manually delete `.zcp/state/launch-production/<launchID>.json` and follow up with `zerops_manage` on orphan project envs.)

#### `kind: "launch-completed"` — terminal success, no further action

The launch finished cleanly (`status="launched"`). The production project exists at `targetProjectId`; no further `action="start"` calls are needed for this launch. Work the launch's checklist (returned at the original `launched` response): wire delivery, ship the first release, verify, then close the launch window via `action="confirm-production"` (deletes the staged `ZCP_LAUNCH_TOKEN` secret; the token-hygiene note recommends regenerating the token afterwards). To launch ANOTHER prod project from the same source, pick a different `productionProjectName` to derive a fresh `launchID`.
