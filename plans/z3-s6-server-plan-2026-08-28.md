# S6 server half — topology feed + lifecycle feed (plan)

Date: 2026-08-28. Stream: `plans/z3-brief-2026-08-28.md` §6 "S6" (server deliverables), D5.
Worktree: `../z3-wt/s6`, branch `z3-s6` (cut from `z3` @ `40b124779`). Fork discipline: brief §4 rule 6.
Facts this plan rests on: `../z3/docs/internals/zerops/verified.md` §§ S0.11, S0.12, S6 PROVE;
`plans/z3-s0-report-2026-08-28.md` § S6. Envelope wire contract: `docs/spec-z3.md` §1 on zcp
branch `feat/z3-envelope-wire` (`ff008e92`, `343bd2d2`).

---

## 1. What the two feeds are

| Feed | Source | Cardinality | Answers |
|---|---|---|---|
| **topology** | `zcp studio topology` (direct read) driven by `zcp studio watch` (doorbell) + a slow poll | one per server = one Zerops project (D3: environment ⇔ project) | *what exists* — the service map |
| **lifecycle** | a reducer over `ProviderService.streamEvents`, extracting the `StateEnvelope` from `zerops_*` tool results | one per thread | *where the agent is* — the strip + cards |

They are independent: neither imports the other. Both read only; neither ever mutates the platform.

---

## 2. Seam map (file:line, all verified in this worktree / this repo)

### 2.1 zcp side (`../zcp`, read-only for this stream)

| Seam | Where | Fact |
|---|---|---|
| topology JSON | `cmd/zcp/studio.go:123-142` `runStudioTopology` | `ops.Discover` → `FillSubdomainURLs` → `tools.EnrichWithMetaStatus` → `emitJSON` (**pretty-printed**, `SetIndent("","  ")`, one object, single trailing newline) |
| topology shape | `internal/ops/discover.go:25-29` (`DiscoverResult{project,services,notes?,warnings?}`), `:102-134` (`ServiceInfo`) | per service: `hostname, serviceId, type, status, adoptionState, isInfrastructure, mountPath?, subdomainEnabled?, subdomainUrl?, created?, containers?, resources?, ports?, envs?, refs?, activity?` |
| **mount state is present** | `internal/tools/discover.go:127-132` | `mountPath` = `/var/www/<hostname>` when that directory exists — set by `EnrichWithMetaStatus` regardless of `stateDir`. So "mounted" comes from zcp's own data; nothing is derived client-side. |
| **activity is NOT present** | `internal/tools/discover.go:110-112` | the exported `EnrichWithMetaStatus` passes `activity = nil`; only the internal `enrichWithMetaStatus` gets a map. Confirms verified.md S0.11 "no live process state". |
| `isInfrastructure` semantics | `internal/ops/discover.go:261` = `topology.IsManagedService(typeVersion)` | it means **managed data service** (postgresql/mariadb/mysql/valkey/keydb/elasticsearch/meilisearch/rabbitmq/kafka/nats/clickhouse/qdrant/typesense/object-storage/shared-storage — `internal/topology/predicates.go:22-27`), *not* "infrastructure" in the POC's sense |
| adoption states | `internal/ops/discover.go:76-81` | `adopted, resumable, adoptable, managed-dep, zcp-self, bootstrapping` |
| doorbell | `cmd/zcp/studio.go:296-330` | NDJSON `{"type":…}` on stdout; **`io.Copy(io.Discard, os.Stdin)` → cancel on stdin EOF** (`:302-305`) — the child's stdin must stay an open pipe or it exits at once |
| doorbell events | `internal/dataconsole/watch/watch.go:1-30` | exactly `connected` \| `topology-changed` \| `disconnected`; fires on list-membership change only (import +0.55 s, delete +10 s); **never on a status transition**; self-heals and re-emits `connected` |
| envelope producer | `internal/workflow/envelope_wire.go` (`feat/z3-envelope-wire`) | `AppendEnvelope` / `ExtractEnvelope` — the reference the TS reducer mirrors |
| envelope type | `internal/workflow/envelope.go:19-198` | `StateEnvelope{phase, environment, idleScenario?, exportStatus?, selfService?, project, services[], workSession?, bootstrap?, generated}` |
| which tools carry it | `docs/spec-z3.md` §1.3 | only `zerops_workflow` `status` / `develop start` / `close`. Every other tool returns `jsonResult` and carries **none** — the strip advances on those three; other `zerops_*` calls contribute only a "last action" name. Errors carry none by design. |
| service statuses | `zerops-go@v1.0.21/types/enum/serviceStackStatusEnum.go` | `NEW ACTIVE CREATING DELETING DELETED STARTING STOPPING STOPPED RESTARTING RELOADING SCALING UPGRADING REPAIRING REPAIR_FAILED MOVING_CONTAINER READY_TO_DEPLOY FAILED ACTION_FAILED CONTAINER_FAILED` (+ `SERVICE_`-prefixed aliases) |

### 2.2 Fork side (`../z3-wt/s6`)

| Seam | Where | Fact |
|---|---|---|
| runtime event bus | `apps/server/src/provider/Layers/ProviderService.ts:234` `PubSub.unbounded<ProviderRuntimeEvent>`, published at `:286-293`, exposed at `:1235-1236` `get streamEvents()` → `Stream.fromPubSub` | one provider-agnostic stream; subscribing is `Stream` consumption, no new seam needed |
| event base | `packages/contracts/src/providerRuntime.ts:252-266` | `{eventId, provider, providerInstanceId?, threadId, createdAt, turnId?, itemId?, …}` — **`threadId` is on every event** |
| item payload | `packages/contracts/src/providerRuntime.ts:409-422` `ItemLifecyclePayload` | `{itemType, status?, title?, detail?, data?: unknown, agentId?, parentToolUseId?}` |
| Claude emit | `apps/server/src/provider/Layers/ClaudeAdapter.ts:2762-2766, 2821-2838` | `data = {toolName, input, result}` where `result` is the **raw `tool_result` block** built at `:1501-1546`: `{type:"tool_result", tool_use_id, content: string \| Array<{type:"text",text}>, is_error?}` |
| Codex emit | `apps/server/src/provider/Layers/CodexAdapter.ts:466-501` | `data = event.payload` = the raw `V2ItemCompletedNotification`; the tool lives at `data.item` = `{type:"mcpToolCall", server, tool, arguments, result?:{content:unknown[]}, error?, status}`; `title` = `"<server> · <tool>"` (`:487-490`) |
| **Claude's `itemType` is unreliable** | `apps/server/src/provider/Layers/ClaudeAdapter.ts:736-771` `classifyToolItemType` | ordered substring match on the tool name: `…delete…` is tested **before** `…mcp…`, so `mcp__zerops__zerops_delete` classifies as `file_change`, not `mcp_tool_call`. See §4 D1. |
| child process, short-lived | `apps/server/src/processRunner.ts:143` `ProcessRunner.run` → `{stdout, stderr, code, timedOut, stdoutTruncated}`; layer `:418` | the house way; `apps/server/src/vcs/VcsProcess.ts:187` shows `Layer.provide(ProcessRunner.layer)` |
| child process, long-lived NDJSON | `apps/server/src/resourceTelemetry/NativeTelemetryClient.ts:534-596` | `ChildProcess.make(..., {stdin:{stream:"pipe",endOnDone:false}, stdout:"pipe"})` + `Effect.acquireRelease` + `Stream.pipeThroughChannel(Ndjson.decode({ignoreEmptyLines:true}))`; backoff `:328`, `:48-49`, supervisor loop `:675-742` |
| snapshot-then-changes | `apps/server/src/utils/subscribeBeforeSnapshot.ts:6-26` + `apps/server/src/resourceTelemetry/ResourceTelemetry.ts:406-412` | the exact template for a `subscribe` that cannot drop an update between snapshot and stream |
| service idiom | `apps/server/src/orchestration/ThreadPlanProgress.ts:29-76` | `Context.Service<Self, Shape>()("t3/…")` + `Layer.effect` |
| per-thread durable blob | `apps/server/src/persistence/ProviderSessionRuntime.ts:34-51, 238-300, 333` | the only precedent for "latest arbitrary JSON per thread" written **outside** the projection pipeline — exactly our write pattern |
| migrations | `apps/server/src/persistence/Migrations.ts:58, 112` | static imports + one `migrationEntries` row; tail today `[43, "ProjectionThreadsUnsettledAt", Migration0043]` |
| contract slice | `packages/contracts/src/resourceTelemetry.ts` + `rpc.ts:209` (`WS_METHODS`), `:435-448` (unary), `:1013-1018` (`stream:true`), `:1020` (`RpcGroup.make`) | the copy-paste template |
| scope registry (type-forced) | `apps/server/src/auth/RpcAuthorization.ts:23-130` (`satisfies Readonly<Record<WsRpcMethod, …>>`) | a new Rpc without a scope is a **compile error**; test `RpcAuthorization.test.ts:13-16` |
| handler site | `apps/server/src/ws.ts:549` (service acquire), `:1742-1757` (unary), `:2464-2483` (stream) | wrappers `observeRpcEffect` / `observeRpcStream` |
| layer composition | `apps/server/src/server.ts:372` (`ProviderLayerLive` inside `RuntimeCoreDependenciesLive`), `:426-436` (`RuntimeDependenciesLive`) | our layers go in `RuntimeDependenciesLive`, so `ProviderService` is already available |
| server cwd | `apps/server/src/config.ts:74` `ServerConfig.cwd` | z3 runs with cwd `/var/www`; the CLI seam uses it |
| tests | `AGENTS.md:106-107` | `vp test run <files>`; **no repo-wide checks** |

---

## 3. Module layout (all new files; three one-line registry touches + one migration row)

```
packages/contracts/src/zerops.ts                    NEW  contract slice
packages/contracts/src/index.ts                     +1   export * from "./zerops.ts"
packages/contracts/src/rpc.ts                       +4   import block, WS_METHODS entries, Rpc.make, RpcGroup.make
apps/server/src/auth/RpcAuthorization.ts            +4   one scope line per method (type-forced)
apps/server/src/ws.ts                               +2/+4 service acquire + handlers
apps/server/src/server.ts                           +1   Layer.provideMerge(ZeropsLayerLive)
apps/server/src/persistence/Migrations.ts           +2   import + migrationEntries row

apps/server/src/zerops/
  envelope.ts            pure — extract the fenced block, decode the mirror
  envelope.test.ts
  serviceTaxonomy.ts     pure — group + transient + mounted derivation
  topologyParse.ts       pure — raw studio JSON → ZeropsTopologySnapshot
  topologyParse.test.ts
  toolResult.ts          pure — ProviderRuntimeEvent → {toolName, text, failed} (both providers)
  toolResult.test.ts
  ZeropsCli.ts           seam — readTopology (short-lived) + doorbell (long-lived NDJSON)
  ZeropsCli.test.ts
  ZeropsTopology.ts      service — PubSub, doorbell fiber, poll fiber, nudge fiber
  ZeropsTopology.test.ts
  ZeropsLifecycleRepository.ts   sqlite, per-thread latest
  ZeropsLifecycle.ts     service — reducer over ProviderService.streamEvents
  ZeropsLifecycle.test.ts
  layer.ts               ZeropsLayerLive composition (the single line server.ts adds)
apps/server/src/persistence/Migrations/0NN_ZeropsThreadLifecycle.ts   NEW
```

---

## 4. Decisions

**D1 — the lifecycle reducer gates on the TOOL NAME, not on `itemType`.**
The brief says "`itemType === "mcp_tool_call"` and a tool name starting with `zerops_`". Claude's
`classifyToolItemType` (`ClaudeAdapter.ts:736-771`) tests `…delete…` → `file_change` before
`…mcp…` → `mcp_tool_call`, so `mcp__zerops__zerops_delete` arrives as `file_change`. Gating on
`itemType` would silently drop it (and any future `zerops_*create*` / `zerops_*edit*` tool).
The name is authoritative and equally cheap: normalise `mcp__<server>__<tool>` → `<tool>`, accept
`zerops_*`. `itemType` is recorded, never used as a gate. A test pins the `zerops_delete` case.

**D2 — the taxonomy is derived from `adoptionState` + `isInfrastructure`, not from the POC's field.**
The POC grouped on `serviceStackTypeInfo.serviceStackTypeCategory`, which `zcp studio topology`
does not carry. Equivalent rule from what it does carry, in order:
1. `adoptionState === "zcp-self"` or `type` starts with `zcp` → **infrastructure**
2. `isInfrastructure === true` (i.e. `topology.IsManagedService`) → **data**
3. otherwise → **runtimes**
That reproduces USER→runtimes / STANDARD+OBJECT_STORAGE→data / else→infrastructure.

**D3 — "transient" is defined by a SETTLED allow-list, everything else is transient.**
Settled: `ACTIVE, RUNNING, STOPPED, READY_TO_DEPLOY, FAILED, DELETED, ACTION_FAILED,
CONTAINER_FAILED, REPAIR_FAILED` (+ their `SERVICE_`-prefixed aliases). A status zcp/the platform
adds later is treated as transient, which costs one extra poll and never freezes the map — the
inverse mistake would leave a service stuck mid-transition on screen.

**D4 — a build in flight is NOT visible to the topology feed, and the plan does not pretend it is.**
`zcp studio topology` carries no process state (§2.1) and the doorbell never fires on status
(S0.11). During a first deploy a service reads `READY_TO_DEPLOY` throughout. So: the topology
feed enters a **nudge window** (poll every 3 s for 90 s, then back to idle) whenever a `zerops_*`
tool completes on the provider bus — the same signal the lifecycle feed reads, subscribed
independently so neither service depends on the other. Build *progress* stays the lifecycle
feed's / the deploy card's job.

**D5 — zcp absence is distinguished from zcp failure.**
Binary missing (spawn `ENOENT`) → `available:false`, `reason:"zcp-not-found"`, no doorbell child,
no poll, no errors, forever (a non-Zerops environment). Command present but failing (auth,
network, non-zero exit) → `available:true, degraded:true` with the stderr tail as `reason`, and
the poll keeps retrying. Deciding "disabled forever" on a transient boot failure would be wrong.

**D6 — the latest envelope per thread IS persisted (a table, not memory).**
The brief requires survival of reconnect and compaction; both are satisfied in memory, but S0.10
proved `state.sqlite` survives a container **restart** — and restart is the product's own "Enable
Zerops Code" path, precisely when a returning client should still see its strip. Cost: one
migration (`0NN_ZeropsThreadLifecycle`), which is the only rebase-fragile touch in the stream (an
id collision with upstream is a mechanical renumber). Write pattern follows
`ProviderSessionRuntime` (direct write from a live stream, not the projection pipeline), because
these events are not part of T3's event log.

**D7 — the envelope mirror keeps enum-shaped fields as `Schema.String`.**
Unknown-field tolerance is not enough: `Schema.Struct` already ignores excess keys, but a
`Schema.Literals` on `phase` / `status` / `adoptionState` / `closeDeployMode` / `gitPushState` /
`runtimeClass` / `buildIntegration` would make the *whole* envelope undecodable the first time zcp
adds a value (it added `launch-production-active` this way). The slice exports
`KNOWN_ZEROPS_PHASES` etc. as const arrays for the client to switch on with a default branch.
Go-produced timestamps stay `Schema.String` for the same reason; timestamps the server itself
mints (`at`, `readAt`) are `Schema.DateTimeUtc`.

**D8 — `recentTools` records EVERY `zerops_*` tool, enveloped or not.**
Only three tools carry an envelope (§2.1), so the envelope alone cannot say "deploying". The ring
(N = 8) records `{itemId, toolName, status, at}` on `item.started` and updates the matching entry
on `item.completed`/`failed`. This is a log, not a state machine — the envelope stays the state
(brief §6 "Don't").

---

## 5. Contract slice (`packages/contracts/src/zerops.ts`)

```
ZeropsServiceGroup      = "runtimes" | "data" | "infrastructure"
ZeropsService           = {hostname, serviceId, type, status, group, adoptionState,
                           isManagedService, mounted, mountPath?, subdomainEnabled?,
                           subdomainUrl?, transient}
ZeropsProject           = {id, name, status}
ZeropsTopologySnapshot  = {available, degraded, reason?, project?, services[], warnings[], readAt}
ZeropsStateEnvelope     = mirror of workflow.StateEnvelope (D7)
ZeropsRecentTool        = {toolName, status, at}
ZeropsLifecycle         = {threadId, envelope?, recentTools[], updatedAt}
```

WS methods (`WS_METHODS` + `RpcAuthorization`, all **read** scope):

| Method | Kind | Payload → Success |
|---|---|---|
| `zerops.topology.get` | unary | `{}` → `ZeropsTopologySnapshot` |
| `zerops.topology.refresh` | unary | `{}` → `ZeropsTopologySnapshot` (forced re-read; the map's manual refresh) |
| `subscribeZeropsTopology` | stream | `{}` → `ZeropsTopologySnapshot` (snapshot-then-changes) |
| `zerops.lifecycle.get` | unary | `{threadId}` → `ZeropsLifecycle` |
| `subscribeZeropsLifecycle` | stream | `{threadId}` → `ZeropsLifecycle` |

The `.changed` event the brief names is the subscription's next element; a separate event method
would duplicate it. `refresh` is read-only (it re-runs `zcp studio topology`), so it stays on the
read scope.

---

## 6. Slices (each RED → GREEN, one commit)

| # | Slice | RED tests |
|---|---|---|
| **1** | contract slice + envelope mirror + `extractEnvelope` | `envelope.test.ts`: table ported from `internal/workflow/envelope_wire_test.go` — one block; **last of two wins**; body not JSON ⇒ ignored (**not** "fall back to the earlier block" — the Go reducer returns false); unterminated last block ⇒ scan further back; fence mentioned mid-line ⇒ text; no block; trailing whitespace after the fence; a real 4-service envelope decodes with an unknown `phase` and an unknown extra field |
| **2** | `toolResult.ts` — provider-shaped extraction | `toolResult.test.ts`: Claude fixture (`data.{toolName,input,result.content:[{type:"text",text}]}`), Claude with `content` as a bare string, Codex fixture (`data.item.{server,tool,result.content}`), non-zerops tool ⇒ undefined, `is_error`/Codex `error` ⇒ skipped, **`zerops_delete` arriving as `itemType:"file_change"` is still accepted** (D1), missing `data` ⇒ undefined |
| **3** | `serviceTaxonomy.ts` + `topologyParse.ts` | `topologyParse.test.ts`: a `zcp studio topology` fixture (zcp-self + 2 runtimes + postgres + valkey, one mounted, one `CREATING`) → groups, `mounted` from `mountPath`, `transient` per D3, unknown status ⇒ transient, `warnings` carried through, malformed JSON ⇒ typed parse error |
| **4** | `ZeropsCli.ts` + `ZeropsTopology.ts` + WS methods | `ZeropsCli.test.ts`: `readTopology` over a real `node -e` stub printing the fixture (the `VcsProcess.test.ts` pattern); doorbell stream over a `node -e` stub emitting NDJSON then **blocking on stdin** — asserts the child is not torn down (proves the `endOnDone:false` pipe, §2.1); child exit ⇒ restart. `ZeropsTopology.test.ts` with a fake `ZeropsCli` layer: initial read publishes; a doorbell `topology-changed` triggers exactly one re-read (**receipt-driven**, a `Deferred` the fake resolves — no sleeps, `AGENTS.md:109`); `ENOENT` ⇒ `available:false` and no further calls (D5); failing command ⇒ `degraded:true` and the poll retries |
| **5** | `ZeropsLifecycleRepository.ts` + migration + `ZeropsLifecycle.ts` + WS methods | `ZeropsLifecycle.test.ts` over `SqlitePersistenceMemory`: events from the two fixtures ⇒ `get(threadId)` returns the latest envelope; a later result **without** a block leaves the envelope untouched but appends to `recentTools`; a corrupted block leaves the envelope untouched; two threads stay independent; a fresh service instance over the same db reads the stored value back (the restart case, D6) |

Verification per slice: `vp test run <the files touched>` + `vp run --filter @t3tools/contracts typecheck`
/ `--filter @t3tools/server typecheck` for the packages touched. No repo-wide checks.

---

## 7. Live checks for main (on the container, after slice 4 / 5)

WS protocol per verified.md S0.9 — effect RPC over JSON arrays:

```
[{"_tag":"Request","id":"1","tag":"zerops.topology.get","payload":{},"headers":[]}]
[{"_tag":"Request","id":"2","tag":"zerops.lifecycle.get","payload":{"threadId":"<id>"},"headers":[]}]
```

1. **topology** — open the WS, send `zerops.topology.get`; expect the project's services grouped,
   `mounted:true` on every `/var/www/<host>` that exists. Then import a service from the GUI:
   the doorbell fires (+~0.5 s) and `subscribeZeropsTopology` delivers a snapshot containing it
   **without a client refresh** (the brief's acceptance line). Delete it: it disappears (+~10 s).
2. **lifecycle** — in a thread, run `zerops_workflow action="status"`; then `zerops.lifecycle.get`
   returns the envelope with the same `phase` the markdown rendered. Run `zerops_deploy` (no
   envelope): the envelope is unchanged, `recentTools` gains `zerops_deploy`.
3. **restart durability** — restart the service, reconnect, `zerops.lifecycle.get` still answers
   (D6). Requires the zcp dev build carrying `feat/z3-envelope-wire`.

---

## 8. Open items / risks

- **Blocked on `feat/z3-envelope-wire` reaching the container** for live check 2-3. Slices 1-5 are
  all provable offline against fixtures; only the live pack needs the zcp build.
- **No real `zcp studio topology` sample yet** — ssh to `z3-eval`'s zcp fails host-key
  verification from this machine (the `Host zcp` entry now points at a different container).
  Fixture is constructed from the Go types (§2.1) until main supplies a live sample; the parse
  test is re-run against it the moment it arrives.
- **Migration id** is the one rebase-fragile line (D6).
- **Out of scope, stated:** nothing client-side beyond the contract; no mutating calls; no
  `.zcp/state` reads; no lifecycle state machine.

---

## 9. What shipped (2026-08-28)

Six commits on `z3-s6`; the first five were merged into `z3` by main as `469e4f179`,
the sixth (`a9a365bf4`) is rebased on top of that merge.

| Commit | Slice | Tests |
|---|---|---|
| `bce8a696a` | contract slice + StateEnvelope mirror + fenced-block reducer | 22 |
| `216622bd1` | provider-shaped tool-call reader (Claude + Codex) | 27 |
| `4789aa59e` | service taxonomy + `zcp studio topology` parser | 36 |
| `2edf060b6` | the zcp process seam (`readTopology` + `watchDoorbell`) | 8 |
| `23b670ecd` | topology feed + `zerops.topology.{get,refresh}` + `subscribeZeropsTopology` | 6 |
| `a9a365bf4` | lifecycle feed + migration 044 + `zerops.lifecycle.get` + `subscribeZeropsLifecycle` | 10 + 2 WS round-trips |

Verified: 175 tests across the zerops module, `RpcAuthorization`, `persistence` and `bin`;
`server.test.ts` 142/142; `tsgo --noEmit` clean on `apps/server`, `@t3tools/contracts` and
`@t3tools/client-runtime`; `vp lint` clean on every touched file. No repo-wide checks.

### Changed from the plan

- **Slice 4 split** into 4a (the CLI seam) and 4b (the feed + WS), for reviewable commits.
- **`Ndjson.decode` rejected for the doorbell** (§2.2 named it as the template). That channel
  FAILS on the first unreadable line, which would silence the doorbell for the rest of the
  child's life over one stray write — and a silent doorbell is invisible, the map would just
  stop updating. Per-line decoding instead (`ManagedEndpointRuntime`'s pattern). Found by a
  failing test, not by inspection.
- **Migration id is 044**, not renumbered — upstream had not moved past 043 at the rebase.
- **`ZeropsLifecycle.ingest` is public.** The background subscription is a thin adapter over
  it, which also lets a test drive the reducer and know when it has settled without a clock.
- **`ZeropsRecentTool` gained `itemId`** — it is what turns a started-then-completed tool into
  one entry that changes status rather than two rows, and it lets the client link a strip entry
  to its timeline row.
- **One client-runtime line** (`EnvironmentSubscriptionRpcTag`): without it a `stream: true`
  method falls into `EnvironmentUnaryRpcTag` and a stream is typed as a unary call. Contract
  correctness, not UI.
- **`RuntimeDependenciesLive` restructured** in `server.ts` (2 lines, not 1). The feeds READ
  from the provider bus and the sqlite store, so they layer on TOP of the assembled runtime;
  listed among its `provideMerge` calls they are treated as a dependency OF it and their own
  requirements leak to every caller. `bin.test.ts` caught it.
- **The timestamp on `ZeropsLifecycle` is `updatedAt`**, not the brief's `at` — `at` is the
  per-entry field on `ZeropsRecentTool`, and two fields called `at` in one payload would be
  ambiguous.

### Known, bounded

- `zerops_thread_lifecycle` rows are never deleted: a deleted thread leaves its row behind.
  A foreign key to `projection_threads` would reject the first write of a brand-new thread
  (the projection lags the live stream), and hooking thread deletion is a different change.
  Cost is bounded — worst case ~1.7 KB per thread that ever ran a Zerops tool.
- The topology fixture is still constructed from the Go types; no live `zcp studio topology`
  sample has been available from this machine.

---

## 10. Coordination round (2026-08-28, after the first `z3` merge)

`c1894ab85` on top of `a9a365bf4`.

- **Two envelope carriers, not one.** JSON-document results (`zerops_deploy`,
  `verify`, `import`, `mount`, bootstrap actions) carry the envelope under a top-level
  `envelope` key. Rule implemented STRICTER than the note: a text that parses as a JSON
  object IS the carrier and its `envelope` key is the only answer — the fence rule is never
  tried on it. A JSON result can carry agent prose in a field and that prose can quote this
  format; falling through would let quoted text become state. Pinned.
- **`recentTools`' justification changed.** "Only three tools carry an envelope" is now
  false. The surviving reason: the envelope says where the agent IS, never what is happening
  now — a running tool has no result yet, a failed one carries none by design.
- **The environment gate uses S1's rule directly.** `ZeropsEnvironment.ts` arrived in this
  base with the merge, so `ZeropsTopology.layer` calls `isZeropsEnvironment(config)`; the
  `// TODO(merge)` helper the note asked for was written and then deleted the same hour. No
  duplicated rule, nothing to dedupe.
- **Real fixtures.** Both live `z3-eval` documents are in `zeropsTopologyParse.test.ts`
  verbatim. They confirmed the taxonomy rule (the live zcp carries `isInfrastructure:false`)
  and exposed one real gap: types carry an OS prefix (`ubuntu/nodejs@22`), so the zcp check
  now strips a leading `<os>/` segment. `warnings[]` is surfaced as an opaque string list,
  never parsed.
- **Renames for merge safety**: `envelope` → `zeropsEnvelope`, `toolResult` →
  `zeropsToolResult`, `topologyParse` → `zeropsTopologyParse`, `serviceTaxonomy` →
  `zeropsServiceTaxonomy`, `layer` → `zeropsFeedsLayer`.
- **The 69 `server.test.ts` failures main saw at `23b670ecd`** were `ws.ts` handlers
  requiring the new tags while `server.test.ts` builds its own layer graph — the app layer
  could not build and the upgrade threw 500. Fixed in `a9a365bf4` by adding `Layer.mock`
  entries to `buildAppUnderTest`. `z3` currently carries the broken commit; the fix is in
  `a9a365bf4`.

Verified after all of it: 364 tests across `server.test.ts` (142), `bin.test.ts`, the zerops
module (169), `RpcAuthorization` and `persistence`; `tsgo --noEmit` clean unfiltered on
`apps/server`, `@t3tools/contracts`, `@t3tools/client-runtime`; `vp lint` clean.
