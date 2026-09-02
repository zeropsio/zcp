# mate — live platform activity on a pending tool card

Status: FRAME + PROVE done, at the SHAPE gate. Companion: `feat/plan-card-merge` in `../z3`
(one PLAN card per bootstrap session, committed `5a5bcc18b`, not pushed).

## 0. The one rule

**The agent's tool result is the truth. Platform activity is an overlay that exists only
while there is no result, is labelled as platform observation, and is discarded the moment
a result arrives.** It never persists, never enters the transcript or the lifecycle envelope,
never changes a card's verdict, and its absence never becomes a claim. Everything below is
that rule applied to each edge.

## 1. What is proven (2026-09-02)

| Fact | Evidence |
|---|---|
| The browser already holds the user's Zerops access token and calls the REST API directly (login, refresh, project list). | `packages/client-runtime/src/zerops/api.ts` `ZeropsApiClient`, `client.session.accessToken`; `ZeropsSessionProvider.tsx` |
| API allows the mate origin. | `curl -X OPTIONS …/web-socket/login -H 'Origin: https://mate.zerops.io'` → `access-control-allow-origin: *`; same on a plain GET |
| One direct, lag-free read gives everything the steps need. | `GET /api/rest/public/project/{id}/process` (zcp `GetProjectProcessesDirect`) returns every process with embedded `appVersion.{status, build.{pipelineStart,startDate,endDate,pipelineFailed,containerCreationStart}, prepareCustomRuntime.{startDate,endDate}, activationDate}` — SDK `dto/output/process.go`, `appVersionJsonObject.go`, `appVersionBuild.go` |
| Hostname → serviceId is already on the client. | `ZeropsTopologySnapshot.services[].serviceId` (`packages/contracts/src/zerops.ts`) |
| The pending tool call's arguments reach the client. | `workEntry.toolData.input` (raw MCP item, `session-logic.ts` `toDerivedWorkLogEntry`) — nothing reads it today |
| Step derivation is a pure function of appVersion. | frontend-legacy `libs/zui/src/build-state-steps/build-state-steps.utils.ts` `getPipelineState` — 5 steps × {waiting, running, finished, failed, cancelled, activating, noop}, driven by status + timestamp presence, no percent |
| Cards are pure functions of the resolved result; pending rows are the generic tool block. | `decode.ts::readZeropsCardSource` requires `resultText`; `SimpleWorkEntryRow` |
| The mate server holds no Zerops token by design. | `spec-mate.md §2.3`, `ZeropsIdentity.ts` |

Not proven, and deliberately not depended on: the platform websocket push (`Process` list/update
subscription). v1 polls the direct read. A doorbell can be added later as a *signal*, never as
the data source.

## 2. Sources and precedence

| Source | Authority | Lifetime | Where it may appear |
|---|---|---|---|
| Tool result (`zeropsResult.resultText`) | authoritative, terminal | persisted (3 projection routes, MF-6) | the card, milestones, merge identity |
| Platform activity (direct process read) | advisory | per viewer, per session, in memory | ONLY the pending branch of a card, labelled "Platform" |
| Topology snapshot | lookup only (hostname → serviceId, project id) | server feed | attribution, never rendered by this feature |

Precedence is total: result > activity. There is no merge of the two. A card with a result
never reads activity (§4 lists the single, explicit exception).

## 3. Attribution — which processes belong to this pending call

A process is attributed to a pending tool call iff ALL hold:

1. **Service**: `process.serviceStacks[].id` (or `serviceStackId`) equals the serviceId the
   topology snapshot maps the tool's target hostname to. Target hostname comes from
   `toolData.input.targetService` (`zerops_deploy`, both the local and the ssh variant —
   `internal/tools/deploy_local.go`, `deploy_ssh.go`). Hostname not resolvable → no attribution.
2. **Project**: `process.projectId` equals the snapshot's `project.id`. Mismatch (wrong API
   host, wrong project) → overlay off for the whole project, quietly.
3. **Time**: `process.created ≥ toolStartedAt − 5 s`. `toolStartedAt` is the server-stamped
   `item.started` event time, never the browser clock. Older processes are "other activity".
4. **Action**: `actionName ∈ {stack.deploy, stack.build}` for `zerops_deploy`. Any other action
   on the same service inside the window (`stack.enableSubdomainAccess`, `stack.restart`) is
   listed as a secondary chip, never as a step source.

All attributed processes are shown (the `LiveOp` rule: never collapse to "the first"). Steps
come from the newest attributed process's `appVersion`; older ones are chips.

## 4. Freeze and the one exception

- **Freeze**: when `resultText` lands, the overlay is discarded, polling for that call stops,
  and the existing card renders exactly as today. Disagreement between the last observation
  and the result is resolved by the result, silently (console debug only).
- **Exception (v1.1, allowlisted)**: a *resolved* deploy whose result status is
  `BUILD_TRIGGERED` (git-push delivery: the tool returns at push time, the build runs after)
  may keep a platform overlay below its verdict line. The allowlist is a constant next to the
  decoder; anything not on it freezes. The overlay stays labelled "Platform" and the card's
  own status stays the result's.

## 5. Per-card state machine

```
idle ──(tool pending ∧ attributable ∧ session)──▶ searching
searching ──(≥1 attributed live/terminal)──▶ observed
observed  ──(all attributed terminal)──▶ settledOnPlatform
searching|observed|settledOnPlatform ──(resultText)──▶ resolved (overlay gone)
any pending ──(401 | 403 | project mismatch | no session | ceiling)──▶ unavailable
observed|settledOnPlatform ──(last good read > 10 s)──▶ stale ──(> 60 s)──▶ unavailable
stale ──(good read)──▶ observed|settledOnPlatform
```

What each state renders on the pending row (kicker "Deploy · <hostname>", status pill
"Running" from the tool lifecycle, unchanged):

| State | Body |
|---|---|
| `idle`, `unavailable` | today's generic pending tool row, byte-identical |
| `searching` | one quiet line: "Platform: no activity for <hostname> yet · <elapsed>" |
| `observed` | 5 steps from `getPipelineState`, "Platform · as of <n>s", other-ops chips |
| `settledOnPlatform` | steps in their terminal state + "Platform reports <finished/failed/cancelled> · waiting for the agent's result" — never a ✓/✗ verdict on the card |
| `stale` | last steps dimmed + "Platform: stale (<n>s)" |

The whole machine is derivable from one read plus the pending entry: no history is needed,
so a reopened thread or a second client lands in the right state on its first poll.

## 6. Poll driver

- Runs iff ≥1 visible pending attributable call exists and a Zerops session is present.
  Interval 2.5 s; on error exponential to 15 s; paused while `document.hidden`; per-call
  ceiling 30 min → `unavailable`.
- One request per project per tick, shared by every pending card in the thread.
- 401 → overlay off (never trigger a Zerops re-login for an indicator); 403/404 → off for
  that project; network error → `stale` path.
- API host: `DEFAULT_ZEROPS_API_BASE` in v1; a later `apiHost` field on the topology snapshot
  replaces it. Project mismatch check (§3.2) guards the interim.

## 7. Edges, each with its resolution

| # | Edge | Resolution |
|---|---|---|
| 1 | Tool knows hostname, not processId | §3: serviceId + server-stamped start − 5 s + action allowlist; all matches shown |
| 2 | Failure before any process (ssh push fails, yaml invalid) | stays `searching` with elapsed; the result brings the error card; ceiling → `unavailable` |
| 3 | ES lag | direct read only, no search, no subscription in v1 |
| 4 | Push unproven | not used; poll; doorbell later as a signal |
| 5 | No percent | 5 steps + elapsed since `build.pipelineStart`; no bar |
| 6 | Build logs | out of scope; separate WS |
| 7 | MCP progress notifications | rejected: provider forwarding unproven; envelope precedent |
| 8 | Agent re-runs deploy inside one call / several appVersions | newest attributed process drives steps, older are chips |
| 9 | User cancels in the GUI | process `CANCELED` → `settledOnPlatform` "cancelled on platform"; verdict still from result |
| 10 | Reload / second client mid-deploy | §5: state from one read; no history needed |
| 11 | Two threads deploy the same service | both attribute the same process; copy says "on <hostname>", never "your deploy" |
| 12 | Wrong API host / project | §3.2 mismatch → off |
| 13 | Interplay with plan-card merge | disjoint: merge identity exists only for resolved results; overlay only for unresolved entries |
| 14 | git-push delivery returns before the build | §4 allowlisted exception, v1.1 |
| 15 | No Zerops session (relay, mobile, logged out) | `idle`; nothing else changes |
| 16 | Token expiry mid-poll | `ZeropsApiClient` refresh path; a final 401 → `unavailable` |
| 17 | Undecodable process DTO (platform adds/renames a field) | total decoder: field-level degrade, whole-row `undefined` → treated as no observation |

## 8. Code shape (`../z3`)

- `packages/client-runtime/src/zerops/activity/` — pure, no React, no Effect:
  `dto.ts` (narrow readers over the process document), `pipelineState.ts` (port of
  `getPipelineState`, table-tested against the FE cases), `attribution.ts` (§3),
  `reducer.ts` (§5, pure `(entry, observation, now) → state`).
- `apps/web/src/zerops/activity/` — `useProjectActivity` poll driver (§6) over
  `ZeropsApiClient`; one atom per project.
- `apps/web/src/components/zerops/ZeropsToolCard.tsx` — a `deploy-pending` payload variant;
  `SimpleWorkEntryRow` gates on `toolLifecycleStatus === "inProgress"` + tool name +
  decodable args.
- Tests: reducer/attribution/pipeline tables; hook with fake timers + fetch mock (401, 403,
  mismatch, stale, hidden tab, ceiling); render: `unavailable` is byte-identical to today's
  pending row; every overlay string carries the "Platform" label.

## 9. Not in v1

`zerops_import` overlay under the PLAN card's provision step (args are a yaml, hostnames
would have to be parsed from it), build-log tail, websocket doorbell, configurable API host.
