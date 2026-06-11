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
| `status` | Phase to expect next response (`ready-to-launch` needs `launchKey`; `launching` / `configuring-pipeline` are polling). |
| `lastUpdate` | Sanity-check freshness — minutes old = active; days old = user may have abandoned (ask before resuming). |
| `ambiguousChoices` | Multiple non-terminal launches exist; pick a `productionProjectName` before resume. |

Resume call shape:

```
zerops_workflow action="start" workflow="launch-production" productionProjectName="<from envelope>"
```

`action="start"` is required on every call — launch-production is stateless multi-call narrowing, `action="start"` is the only orchestration entry (no `action="classify"`, no `action="complete"`). The handler re-reads accumulated state from `productionProjectName` and advances to the next phase.

The `launchKey` is NOT required at the status step — only generate and pass it when the workflow re-enters `ready-to-launch` and you intend to advance to `launching`. Status is read-only; ZCP never constructs a project-admin client on this path.

#### `kind: "launch-failed"` — terminal failure, reset required

The most-recent launch for this source project ended in `failed` (e.g. schema validation rejected the import, mutation API error). The state file persists so the operator can inspect; a blind retry on the same `productionProjectName` would hit cached state and burn a fresh `launchKey` without re-entering the mutation phase.

Recovery: `action="reset"` clears the state file and orphan project envs so the next `action="start"` enters cleanly with a fresh `launchKey`.

```
zerops_workflow action="reset" workflow="launch-production" productionProjectName="<from envelope>"
```

`action="reset"` is destructive — the first call returns a `wouldDestroy` diagnostic listing what will be removed; the second call must echo it back as a structured `confirmDestructive={"operation":"launch-production-reset","acknowledgedTargets":[...]}` ack (matching the `wouldDestroy` payload) to clear the gate. The orphan production project is deleted too when the launch token resolves — from the staged `ZEROPS_TOKEN_PROD` secret on the source push service (no re-send needed), or via an explicit `launchKey` when the staged secret is gone. Without any token, reset only clears local state and leaves the billable orphan for manual dashboard deletion. (Old binaries without reset support: manually delete `.zcp/state/launch-production/<launchID>.json` and follow up with `zerops_manage` on orphan project envs.)

#### `kind: "launch-completed"` — terminal success, no further action

The launch finished cleanly (`status="launched"`). The production project exists at `targetProjectId`; no further `action="start"` calls are needed for this launch. Work the launch's checklist (returned at the original `launched` response): wire delivery, ship the first release, verify, then close the launch window via `action="confirm-production"` (deletes the staged `ZEROPS_TOKEN_PROD` secret; the token-hygiene note recommends regenerating the token afterwards). To launch ANOTHER prod project from the same source, pick a different `productionProjectName` to derive a fresh `launchID`.
