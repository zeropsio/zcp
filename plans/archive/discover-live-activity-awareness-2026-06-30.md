# Discover: live in-flight activity awareness

**Status:** SHIPPED — 2026-06-30. Implemented P0–P6 end-to-end: `ops.ProjectActivity`
primitive (+ `IsProcessLive` broadened to the full non-terminal SDK set per a codex
loop-safety review), discover `activity` surfacing + busy "wait, then adopt" steer, the
`ADOPT_TARGET_BUSY` adopt gate (scope ∪ plan, `GetProcess`-freshened), live e2e
(`TestInFlightActivity_*`), the `seed=building` flow-eval scenario, atom +
`spec-workflows.md §3.5` + CLAUDE.md trap. Design was **live-verified** (eval-zcp probe,
raw shapes below) and cross-checked by an independent Codex design pass.

**Trigger:** a recipe-created project boots an agent whose first `zerops_discover` reports
`appdev`/`appstage` as `READY_TO_DEPLOY` + `adoptable` and steers "adopt now" — while their FIRST
deploy pipeline is actively running. Correct behaviour: detect the live deploy, steer "wait until
it settles, THEN adopt," and refuse a premature adopt.

---

## 1. Root cause (one missing fact)

`zerops_discover` decides each service's state from exactly two inputs: the service's **resting
`status`** (`ListServices`) and **ZCP's local state files**. It never asks the platform the one
question that disambiguates them: **"is a live, non-terminal process running on this service right
now?"** — although the platform exposes it via `SearchProcesses` + `SearchAppVersions` (both already
wrapped by `ops.Events`).

`READY_TO_DEPLOY` means only "no app version has ever activated." That is true for a genuinely idle
service AND for one whose first deploy is running this second. Discover collapses both into
`adoptable → adopt now`, so it steers the agent to adopt a service mid-first-deploy.

The existing `InFlightBootstrapHostnames` doesn't catch it: it is local-session-state only (knows
ZCP's OWN bootstrap is provisioning). The recipe project was created by the Zerops GUI, not this
session — so no local marker exists, and the running build is invisible.

---

## 2. Live-verified ground truth (eval-zcp, first-ever `buildFromGit` deploy)

One `probe` runtime, polled every 2s through a complete first deploy (T0 = import):

```
service.status     stack.build proc   appVersion.status
READY_TO_DEPLOY    RUNNING            WAITING_TO_BUILD   (at import, ~1-2s)
READY_TO_DEPLOY    RUNNING            BUILDING
READY_TO_DEPLOY    RUNNING            DEPLOYING
CREATING           RUNNING            DEPLOYING
ACTIVE             RUNNING            ACTIVE
ACTIVE             FINISHED           ACTIVE             (~1s after ACTIVE)
```

Load-bearing facts:
- **A single `stack.build` process spans build AND deploy** — `RUNNING` throughout, `FINISHED` only
  ~1s after the service reaches `ACTIVE`. There is no separate "deploy" process. Phase granularity
  (build vs deploy) comes from the **appVersion** status, not the process.
- Process status: `PENDING`(ms) → `RUNNING` → `FINISHED`. AppVersion: `WAITING_TO_BUILD` →
  `BUILDING` → `DEPLOYING` → `ACTIVE`.
- **Cross-reference works.** `AppVersionEvent.serviceStackId` is the target runtime (direct, and ZCP
  maps it). `ProcessEvent.serviceStacks[]` contains the target `{id,name}` plus the ephemeral build
  container (different id, `category:BUILD`). ZCP maps `ServiceStacks[].ID`+Name but DROPS the
  category — irrelevant here: matching the **known target serviceID** is unambiguous (the build
  container's id never equals the target's). **⇒ no platform mapper change needed.**
- **THE LOOP TRAP (verified):** a config-broken build fails in **0.27s** as `stack.build=FAILED`
  with the appVersion frozen at `WAITING_TO_BUILD`. "A stack.build exists" therefore does NOT mean
  "a build is running." Same <1s fast-fail class as the `.git`-suffix repo-not-found.

---

## 3. Design — the smallest correct shape

> Add ONE fact ("is a live process on this service?"), surface it in discover, and hard-gate the
> ONE operation where acting on it is premature (adopt). No new layer, no new vocabulary package,
> no web of gates.

### 3.1 The loop-safe predicate (this is the whole correctness argument)

**The process is the SOLE busy-truth. The appVersion is only a phase label.** (Codex review
correction: an appVersion arm that can mark a service busy *on its own* deadlocks — an appVersion
stuck at `BUILDING` whose build container died has no `processId`, so there is no cancel-escape and
the gate never opens. The live capture shows the `stack.build` process is `RUNNING` the entire
build+deploy window, so process-only detection loses nothing.)

A service is **busy** iff a `ProcessEvent` referencing the target serviceID has
`status ∈ {PENDING, RUNNING}` (`isProcessInProgress`). This single arm catches the live
`stack.build` across build AND deploy, the brief `WAITING_TO_BUILD` pre-build window (process is
already `RUNNING` there — live-verified i=01), AND any lifecycle op (restart/scale/stop/start/import).

The target's latest `AppVersionEvent` (by `serviceStackId`, skipping `Source=="NONE" && Build==nil`
startWithoutCode stamps; latest = created-desc first match, the existing `failed_context.go`
selection) is consulted ONLY to refine the **label** of an already-busy service: `BUILDING` →
`action:"build"`, `DEPLOYING` → `action:"deploy"`. It never makes a service busy by itself, so an
old/stuck in-progress appVersion row can't false-gate.

**Why this cannot loop — by construction, not by hope:**
- "Busy" derives ONLY from a **live process** (`PENDING`/`RUNNING`). Every live process resolves to
  a terminal state on its own; at that moment the service stops being busy and the gate opens.
- Every busy verdict therefore carries a real `processId` → the refusal always names it → the agent
  can `zerops_process action=cancel` a genuinely-stuck process and proceed. The escape hatch IS the
  recovery pointer; no `force` flag, no stale-age timer.
- A **failed / stuck build is never busy**: a failed `stack.build` is `FAILED`/`FINISHED`/`CANCELED`
  (not `PENDING`/`RUNNING`); the <1s config fast-fail leaves `stack.build=FAILED` with the appVersion
  frozen at `WAITING_TO_BUILD` — neither is busy. So a service with failed history stays adoptable
  and re-deployable; **recovery is never gated** (the CLAUDE.md hard rule). Exactly the trap from §2,
  neutralized: we never key on mere stack.build presence, only on live process `status`.

### 3.2 One primitive (`ops`), no new topology type

```go
// internal/ops/activity.go
type ServiceActivity struct {
    Action    string `json:"action"`    // build|deploy|restart|scale|start|stop|import
    Status    string `json:"status"`    // BUILDING|DEPLOYING (phase) else the process status
    ProcessID string `json:"processId"` // the live process — always present (busy ⟺ live process); watch/cancel handle
}

// ProjectActivity returns hostname -> ServiceActivity for every BUSY service (a live PENDING/RUNNING
// process references it). Two project-scoped searches in parallel (the ops.Events pattern); takes
// the caller's already-built id->hostname map (no extra ListServices). Idle services are absent.
// Single owner — discover + the adopt gate both call it.
func ProjectActivity(ctx, client, projectID string, idToHost map[string]string, limit int) (map[string]ServiceActivity, error)
```

The struct lives in `ops` next to `ServiceInfo`/`DiscoverResult`; its only consumers (the discover
tool + the adopt tool handler) are L4 and import `ops` freely — so no `topology` type and no
layering detour. The process arm reuses the existing `isProcessInProgress` (PENDING/RUNNING); the
label refinement extracts `isAppVersionInProgress` ({BUILDING, DEPLOYING}) as the shared owner next
to `appVersionHintMap` / `failed_context.go` (no drift). `ProcessID` is non-optional: a busy verdict
always has a live process behind it (that's the loop-safety invariant), and it is the cancel-escape.
`idToHost` is built from the caller's `result.Services` — no fake `ServiceRef` type, no extra fetch.

### 3.3 Touch point A — discover surfaces it (read-only)

`ServiceInfo` gains `Activity *ServiceActivity \`json:"activity,omitempty"\`` (absent when idle —
"surface once, don't dump"). In `enrichWithMetaStatus` (the discover-only tools-layer step;
`env action=get` does NOT call it, so the hot path is untouched), after classification, fetch
`ProjectActivity` and partition the adopt-candidate set:
- **adoptable + idle** → existing "run route=adopt now" warning (unchanged).
- **adoptable + busy** → ONE replacement steer naming what to watch:
  > `appdev`, `appstage` have a deploy in progress (build BUILDING). Wait until status is
  > RUNNING/ACTIVE (re-run `zerops_discover`, or watch `zerops_events serviceHostname=<svc>`),
  > then adopt.

Discover stays `ReadOnlyHint`/`IdempotentHint` — it never waits or polls; the agent polls, bounded
by the build completing. Pagination: `limit = max(100, len(services)*5)` (the fixed-50 of ops.Events
can evict in-progress rows on a busy project).

### 3.4 Touch point B — the gate, on adopt ONLY (the "wait" harness Karel wants)

In `handleBootstrapComplete` (route=adopt, step=discover), **before EITHER dispatch branch** writes
meta, fetch `ProjectActivity` and refuse if any resolved target is busy:
> `appdev` has a build in progress (BUILDING, processId=…). Wait for it to reach RUNNING/ACTIVE
> (watch `zerops_events serviceHostname=appdev`), then re-run adopt. (Cancel a stuck build with
> `zerops_process processId=… action=cancel`.)

**Targets resolve from BOTH paths (Codex catch — this is the actual recipe flow):**
`handleBootstrapComplete` has two adopt dispatches — empty plan → `BootstrapCompleteAdoptPlan(scope)`
(workflow_bootstrap.go:71), explicit `plan=[...]` → `BootstrapCompletePlan` (line 74). The recipe
case `appdev`+`appstage` is a same-stack pair, so discover STEERS it to explicit `plan=[...]`
(the `adoptPairingChoice` flow) — a scope-only gate would miss the reported path entirely. The gate
resolves the target hostname set from `input.Scope` ∪ the dev+stage hostnames in `input.Plan`, and
sits ahead of both branches.

No meta is written on refusal. Because "busy" is live-only (§3.1), a FAILED/terminal target is never
busy → adopt + corrective deploy after a terminal failure stay open. The gate lives in the tools
handler (L4, imports both ops and workflow) before the workflow mutation, mirroring the existing
`requireAdoption` / `repoDeliveryRedirect` preflights — so `workflow` never sees the activity type.
(`resume` passes through the same handler and is fine; `adopt-local`/`LocalAutoAdopt` do NOT route
here and are deliberately ungated — no container-deploy-race evidence.)

**Deliberately NOT gated (scope discipline — "jen tam kde je potřeba"):** deploy / restart / scale.
Deploy already requires adoption, and adoption is now gated, so the reported flow is covered
transitively; a deploy onto an already-adopted-but-busy service is *surfaced* via the discover
`activity` field but not hard-refused in v1. One gate ⇒ no interacting gates that could deadlock
each other. Trigger to add a deploy-busy gate: eval/real evidence of agents deploying onto a live
build and breaking it.

### 3.5 Coexistence with `InFlightBootstrapHostnames` (kept, complementary)

| Signal | Source | Means | Effect |
|---|---|---|---|
| `InFlightBootstrapHostnames` | local session state | "MY alive session is provisioning this" (fires in the import→meta window, before a platform process is even queryable) | `adoptionState=bootstrapping`, silent |
| `ProjectActivity` | live platform | "a process is running on this service, whoever started it" | `activity` field + "wait" steer + adopt gate |

They layer: a service that is BOTH in-flight (my session) AND building stays `bootstrapping`
(silent, the agent's own flow); a busy-but-not-in-flight (external) service gets the new steer. The
classifier already checks `inFlight` first; activity only refines the `adoptable` branch.

---

## 4. Post-implementation knowledge (write it as the state, not the change)

### Atom (`internal/content/atoms/…`) — observable state + pitfall, terse

> **A service can be mid-deploy while its status reads `READY_TO_DEPLOY`.**
> `zerops_discover` carries a per-service `activity` object whenever a build or deploy is live on
> it. A runtime doing its first deploy reads `status:"READY_TO_DEPLOY"` with
> `activity:{action:"build", status:"BUILDING"}` (or `"DEPLOYING"`), passes through `CREATING`, and
> flips to `ACTIVE` only once the deploy activates. A service carrying `activity` is not idle — wait
> for the field to clear (re-run `zerops_discover`, or watch `zerops_events serviceHostname=<svc>`)
> before adopting or deploying onto it.

(State, not handler-verbs: the atom describes what discover *shows*; the adopt refusal is the gate's
own TELL, not an atom. No spec IDs, no env-shaped paths.)

### Spec — `docs/spec-workflows.md`, new short section under discover/adopt

> Discover reports a per-service `activity` derived from live process search. "Busy" = a live
> `PENDING`/`RUNNING` process referencing the service (the sole busy-truth — it always carries a
> cancelable `processId`); the latest appVersion `BUILDING`/`DEPLOYING` only labels the build/deploy
> phase. Terminal/failed states (`FAILED`, `BUILD_FAILED`, `WAITING_TO_BUILD`, `ACTIVE`) are never
> busy, so adoption + corrective deploy after a failure are never gated. Adopt refuses a busy target;
> deploy is covered transitively (adoption-gated).

### CLAUDE.md — ONE trap line (the invariant an agent can break at a new gate site)

> Service "busy" = a LIVE process (`PENDING`/`RUNNING`) referencing it — the SOLE busy-truth (it
> always carries a cancelable `processId`). The appVersion `BUILDING`/`DEPLOYING` is only a phase
> LABEL, never an independent busy signal — an appVersion-only gate deadlocks (stuck `BUILDING`, no
> process to cancel). Never key on mere stack.build presence / `WAITING_TO_BUILD`/`FAILED` (a <1s
> config-fail build is `FAILED` with appVersion frozen at `WAITING_TO_BUILD`); gating those deadlocks
> recovery. `ops.ProjectActivity` is the single owner; the adopt gate and discover steer both read it.

---

## 5. Execution protocol — LIVE-FIRST, then TDD, then real test layers

**Mandate (Karel):** know how it really behaves BEFORE writing a mock. Every mock encodes a shape
captured live on the zcp container in P0; no mock is written from a guess. Three test layers ship:
unit/mock (TDD), live **e2e** (`-tags e2e`), behavioral **flow-eval** (real agent on the container).
TDD is mandatory (RED→GREEN→REFACTOR); verify each phase green before the next; ≤5 files/phase;
plan-fidelity rule applies (report per-item, ask before any scope cut).

| Phase | Work | Gate |
|---|---|---|
| **P0 — LIVE re-verify** (no prod code) | On eval-zcp via the `platform-verifier` agent / `ssh zcp`: provision a throwaway `buildFromGit` runtime (`zeropsio/recipe-nodejs-hello-world`, `zeropsSetup=helloworld`, the hostname≠setup fast-fail gotcha applies), poll `process/search` + `app-version/search` + `service-stack/{id}` every ~2s through a full first deploy. Confirm the §2 timeline (esp. `stack.build` `RUNNING` spanning build+deploy; service `READY_TO_DEPLOY` throughout; the <1s FAILED fast-fail leaving appVersion `WAITING_TO_BUILD`). Freeze the raw JSON as the mock fixtures. **Delete the probe.** | §2 reconfirmed (flag ANY drift); fixtures saved under `internal/ops/testdata/` |
| **P1 — ops primitive** (unit/TDD) | RED `ops/activity_test.go` from the P0 fixtures, then GREEN `ops/activity.go` (`ServiceActivity{Action,Status,ProcessID}`, `ProjectActivity(idToHost)`, `isAppVersionInProgress`) + `ServiceInfo.Activity`. Cases: process-RUNNING+appVersion-BUILDING ⇒ busy `action:build`; DEPLOYING label; lifecycle restart (process-only); idle; **FAILED stack.build + `WAITING_TO_BUILD` ⇒ NOT busy**; **appVersion stuck `BUILDING`, NO live process ⇒ NOT busy** | `go test ./internal/ops/...` green |
| **P2 — discover enrich** (tool/TDD) | RED `discover_inflight`/`discover_adopt_hint` siblings (`READY_TO_DEPLOY`+RUNNING-build ⇒ `adoptable` + `activity` + wait-warning, NOT adopt-warning; idle adoptable unchanged) + `"activity"` added to the `env_get_response_test` leak-guard, then GREEN: `enrichWithMetaStatus` fetches `ProjectActivity`, attaches, partitions the warning | tool + classifier-parity green |
| **P3 — adopt gate** (tool/TDD) | RED `workflow_bootstrap_adopt` tests (busy target ⇒ refusal naming processId + watch, no meta, session not advanced; **explicit `plan=[...]` path gated**, not just scope; FAILED/terminal target ⇒ adopt proceeds), then GREEN: gate ahead of both dispatch branches resolving `Scope ∪ Plan` | tool green; full `go test ./... -race` green |
| **P4 — e2e** (real platform) | `e2e/inflight_detection_test.go` (verifier spec): provision `buildFromGit` probe, assert the live timeline + the ID cross-reference + the FAILED-fast-fail negative, `DELETE` + assert gone | `go test ./e2e/ -tags e2e -run InFlight` green on eval-zcp |
| **P5 — flow-eval** (real agent) | New `eval/behavioral/scenarios/recipe-first-deploy-race-*.md`: harness kicks a `buildFromGit` import then boots the agent immediately so its first `zerops_discover` lands mid-build; observe the agent reads `activity`, WAITS (re-discovers / watches events), adopts only after settle, and **never escapes to `zcli`**. Run `./eval/behavioral/flow-eval.sh <id>`; surface the retrospective | agent waits, no premature adopt, no zcli escape |
| **P6 — knowledge** (post-state prose) | atom (§4, observable state, post-impl), `docs/spec-workflows.md` section, ONE CLAUDE.md trap line, archive this plan | atom lint + `make lint-local` green |

**Backward-compat:** `activity` is a new `omitempty` field (old parsers unaffected); `adoptionState`
unchanged; `InFlightBootstrapHostnames` kept; env-get response structurally excludes it. Internal-only.

**Effort:** ~4 code files + 3 test layers + 1 atom + 1 spec/CLAUDE line; ~300–400 LOC incl. tests; ~3 days.

### RED fixture (verbatim live capture, eval-zcp 2026-06-30)

In-flight ProcessEvent (BUILDING window) — note target `probe` (USER) + build container (BUILD) in `serviceStacks[]`:
```json
{ "actionName":"stack.build", "id":"noEoVpIZRjKRBB5dRf0xRA", "status":"RUNNING",
  "started":"2026-06-30T08:11:10.215Z", "finished":null,
  "serviceStacks":[ {"id":"fABfxO2hSvALnMPKioCU8g","name":"probe"},
                    {"id":"GzowBYM6RGCNCcXxD4YAtw","name":"buildprobev1782807070"} ] }
```
In-flight AppVersionEvent (same window): `{ "id":"oVlyjE8CSoao5hoLGsAOAg", "status":"BUILDING", "source":"GIT", "serviceStackId":"fABfxO2hSvALnMPKioCU8g", "activationDate":null }`
Terminal: process `status:"FINISHED"`; appVersion `status:"ACTIVE"`, `activationDate:"2026-06-30T08:11:50Z"`.
Raw snapshots: `…/scratchpad/zcp-verify/poll2/{proc,av,svc}-*.json`.

---

## 6. Resolved (incl. Codex plan-review must-fixes)

1. ~~Detection: process-primary vs appVersion-primary vs both~~ → **process is the SOLE busy-truth**
   (always carries a `processId` → loop-safe cancel-escape); appVersion `BUILDING`/`DEPLOYING` is a
   pure phase *label*, never an independent busy signal (kills the appVersion-stuck-no-process
   deadlock Codex found). Live-verified: the `stack.build` process is `RUNNING` the whole window.
2. ~~Adopt gate covers only `scope`~~ → **must cover explicit `plan=[...]` too** (Codex): the recipe
   `appdev`+`appstage` same-stack pair is steered to `plan=[...]`; gate resolves `Scope ∪ Plan`
   targets ahead of both dispatch branches.
3. ~~Adopt gate A (note) vs B (refuse)~~ → **refuse** (Karel wants the clear "wait" harness); safe
   because busy is live-only.
4. ~~New topology type? `ServiceRef`? `Since` field?~~ → **no** — `ops`-local struct
   `{Action, Status, ProcessID}`, `idToHost` map (no fake ref type), gate in tools handler.
5. ~~Deploy/restart/scale gates?~~ → **deferred** (surfaced not gated; one gate avoids deadlock).
6. ~~Platform mapper change for process category?~~ → **no** — match by known target serviceID
   (build-container id never collides).
7. Cost: always-fetch in discover (env-get confirmed unaffected — it doesn't call
   `enrichWithMetaStatus`). Optional lever if discover latency bites: fetch only when ≥1 adoptable
   runtime candidate (loses the activity field on adopted-but-busy services — case #2).
