# ZCP Session & Lifecycle — Independent Review VERDICT (2026-05-29)

Reviews `plans/zcp-session-lifecycle-REVIEW-PACKET-2026-05-29.md` (the "spec-ready" design).
Method: code/test re-verification + a 40-agent workflow (ground → stress → simplify → adversarial
verify → synthesis, 3.7M tokens). Authority order honored: **tests > code > spec > plans** — every
claim below is checked against the cited file:line, and where tests/spec contradict the design, the
test wins.

---

## Verdict: **SIMPLIFY-THEN-SHIP**

The **problem is real** (8/8 of the packet's §1.3 table verified). The design's **core resolution
moves are correct**. But it is **over-built in ~1/3 of its machinery**, has **two blockers** (one of
them an owner decision that reverses a stated "hard requirement"), and one **uncompilable layering
claim**. Cut the imagined-threat machinery, fix the two close/scope fixpoints, and ship the concept.

---

## 1. Problem — CONFIRMED (do not re-litigate)

| ID | Status | Decisive evidence |
|---|---|---|
| SPINE-1 | confirmed, triggerable | dispatcher reads **in-memory** `e.sessionID` (`workflow.go:572`→`handleBootstrapStatus`, never reads work file); envelope `derivePhase` reads **disk** work-first (`compute_envelope.go:398-411`). Reachable: develop writes a per-PID work **file** w/o an engine session (`workflow_develop.go:250-257`); bootstrap sets `e.sessionID` (`engine.go:366`); engine is one long-lived per-process object. **Nuance the packet understates:** the two precedences don't fight *within one status call* (the bootstrap branch shields `derivePhase`) — the contradiction is **cross-surface** in one on-disk state. |
| SPINE-3 | confirmed, narrow | `_ = updateRegistryPID(...)` `engine.go:148`; only on the recovery path. |
| SPINE-4 | confirmed | `InitSessionAtomic` doc-comment (`session.go:181`) claims a bootstrap-exclusivity check; body `:203-227` does none. **But see Blocker 1** — the real hole is the pair-unaware hostname lock, not a missing *project* singleton. |
| XCUT-1 | confirmed, triggerable | `WriteServiceMeta` `os.Rename`-only, no lock; **15** RMW callers; `jsonrpc2.Async` concurrent dispatch + cross-tool sharing of one `.zcp/state`. `work_session.go:90-91` comment ("serialized by the server") is WRONG; `engine.go:19-31` is right. |
| DEV-1 | confirmed | `RecordDeployAttempt` returns `ErrHostnameOutOfScope` `work_session.go:201`; callers `_=`-discard it. |
| PID-reuse | confirmed | `isProcessAlive` reads `EPERM`-as-alive, no start-time `registry_unix.go:45-51`. |
| launch coupling | confirmed | `launch_state.go` has zero PID fields; dispatcher status preempts develop at `workflow.go:453` before the develop path at `:467`. |
| R7 | confirmed verbatim | spec §5.3/§9.3 "bootstrap PRIMARY" vs §6.2 step 1 "if work session exists → Work Session status". Upstream cause of SPINE-1. |

---

## 2. KEEP — correct, structural, not over-engineering

1. **One `ResolveLifecycle` consumed by both the dispatcher and `ComputeEnvelope`** — the right SPINE-1
   fix. *Load-bearing detail the packet under-emphasizes:* the fix is **removing `e.sessionID` as the
   precedence source** so both paths read disk — not merely "adding a resolver."
2. **Focus-rule reconciliation of R7** (`operatesInfraOp → infra, else hasWork → work, else idle`) —
   makes §5.3 and §6.2 both true as two focus states of one machine. Cleaner than the audit's flat
   "strike §6.2."
3. **`state.Update`-style flock-guarded RMW as the single write path for ServiceMeta** — the correct
   XCUT-1 fix. The **flock is load-bearing** (parallel tool_use *and* cross-tool); a bare Go mutex is
   insufficient. 15-caller count accurate.
4. **`(pid, startTime)` identity** for operator AND work — fixes the EPERM/PID-reuse class. (Keep the
   identity; simplify the liveness states — §4.)
5. **Launch is project-scoped, any-PID-recoverable, demoted from the status-preempt** — correct; it
   should no longer hide develop. (Render as a uniform project element, not a special "overlay" noun.)
6. **Auto-COMPLETE as a DERIVED phase** (delete `MaybeFireAutoClose` + per-surface lazy-fire) — the
   gate predicate (`EvaluateAutoClose`/`serviceAutoCloseReady`/`autoCloseGateOpen`) is *already* pure
   over disk state and shared with the display path. The `status`-doesn't-fire-the-gate desync is the
   strongest argument FOR deriving.
7. **Dynamic footprint as the DEV-1 fix** — correct, *provided* additive over a declared baseline +
   success-gated (see Blocker 2).
8. **Clean migration** (no mixed-fleet bridge) and **R7-first build order** — appropriate.

---

## 3. BLOCKERS (2)

### Blocker 1 — "bootstrap is a project singleton" reverses the pinned contract → **OWNER DECISION**
The packet's hard-requirement #2 (`≤1 active bootstrap per project`) and invariant **I2** **contradict
authoritative tests + spec**:
- `TestEngine_BootstrapParallelAllowed` (`engine_test.go:204`): *"Second bootstrap on different engine
  should succeed (per-service, not global)."*
- `TestInitSessionAtomic_BootstrapExclusivity / second_bootstrap_allowed` (`engine_test.go:1399`,
  `expectErr=false`).
- `docs/spec-workflows.md:419` + B6 (`:973`): *"Per-service exclusivity via hostname lock. Multiple
  bootstraps coexist for different services. Same-hostname lock: incomplete ServiceMeta from alive
  session blocks new bootstrap for that hostname. Dead PID → auto-unlock."*

So the design presents a **behavior change as a bug fix**. SPINE-4's *real* hole is narrow:
`checkHostnameLocks` (`engine.go:726`) resolves via non-pair-aware `ReadServiceMeta`, so a stage-half
hostname returns nil and **slips the existing per-hostname lock**. Fix = make it pair-aware
(`FindServiceMeta`) + fix the stale `session.go:181` doc-comment.

**Why it matters architecturally:** the project-singleton is the thing **most of the operator-CAS-
takeover + tri-state-liveness apparatus exists to defend.** Keep exclusivity **per-hostname** and that
apparatus largely **dissolves** (a hostname lock + the existing flock + dead-PID prune already passes).

> **Decision for Karel:** is the project-singleton a *deliberate new* behavior (then update the tests +
> spec B6, and accept the takeover machinery) — or should exclusivity stay **per-hostname** (then drop
> the singleton and most of the machinery)? The review **recommends per-hostname** (cheaper, matches
> the pinned contract, the develop⇄bootstrap nesting requirement does not need a project lock).

### Blocker 2 — dynamic footprint without an upfront denominator fires auto-close prematurely
Scope is a **required explicit input today** (`workflow_develop.go:222`: *"Scope is a required explicit
input at start. No derivation… no fallback."*); `EvaluateAutoClose` returns false on
`len(ws.Services)==0` and uses `ws.Services` as the denominator. If footprint = "deployed-so-far," the
**first** successful deploy+verify of ONE service makes the footprint trivially all-green → derived
close fires while a multi-runtime task still has undeployed services.
**Fix:** keep **declared scope as the auto-close denominator**; re-scope "dynamic footprint" to the
**additive DEV-1 fix only**: `footprint = union(declared, successfully-deployed)`; derived close = **all
of DECLARED** green. Pin: `scope=[web,api]`, deploy+verify `web` only → phase stays `develop-active`.

---

## 4. CUT — over-engineering (sized to threats ZCP doesn't have / duplicates existing infra)

| Cut | Why |
|---|---|
| **The new `sessions/infra-lock.json` file + new lock primitive** | The registry is *already* the project-scoped, flock-guarded (`withRegistryLock`: lock→read→mutate→atomic-write, timeout→error, **no degrade-to-unlocked**), PID+workflow+project store with a dead-PID auto-claim. Bootstrap is *already* a registry session. Adding infra-lock.json is the **4th uncoordinated store R1 warns against** and re-opens SPINE-3 (double-bookkeeping). **Lift `withFileLock(path, fn)` out of registry.go**; express work-write, hostname-claim, meta-RMW as 3 callers of it. |
| **The in-process mutex-map in `state.Update[T]`** | A perf cache, not correctness. `withRegistryLock` is **flock-only** and is the design's own cited precedent; flock serializes same-process goroutines AND cross-process. Add a mutex-map later only if profiling shows contention. |
| **Generic `state.Update[T]` + a new `internal/workflow/state` package framing** | Over-abstraction before a 2nd locked type needs it. ServiceMeta is the one genuinely-unlocked shared store; lock it directly via the lifted `withFileLock`. |
| **Tri-state liveness as a permanent `unknown ⇒ refuse` wedge** | The "cross-user EPERM" rationale is **empirically false on both platforms** (Darwin `SysctlKinfoProc` reads cross-user incl. root; Linux `/proc/<pid>/stat` field 22 is world-readable). Keep `(pid,startTime)` two-state + the existing 24h `pruneDeadSessions` TTL backstop. If "unknown" is kept, make it **bounded-retry-then-treat**, never an indefinite wedge (`reset` is PID-scoped and cannot clear a foreign lock). |
| **`I11` (bootstrap↔recipe mutual exclusion "preserved from e.Start")** | Does **not exist at runtime.** Live v3 recipe is `zerops_recipe` backed by an in-memory `recipe.NewStore` (`server.go:166`); it never calls `e.Start`. The only `e.Start` recipe path (`RecipeStart`) has **zero live callers** (`workflow=recipe` is rejected at `workflow.go:552`). Delete the claim. |
| **FOCUS/STORAGE as *persisted runtime structures* + the "LaunchOverlay" noun** | FOCUS is a pure-function output; STORAGE is the on-disk directory layout. They're a good *lens*, not two carried data structures. The "overlay becomes-primary when no work" rule re-introduces a split-precedence — render launch as **one uniform project lifecycle element** (focus element first, then project elements); this handles N>1 launches for free. |

---

## 5. FIX — implementability holes the packet glosses

- **MAJOR (uncompilable as written):** `launch_state.go` is `package tools` (layer 4); `.golangci.yaml:136`
  `workflow-not-ops` forbids `workflow/` importing `tools/`. So "`ResolveLifecycle` (workflow) returns
  `LaunchOverlay`; `ComputeEnvelope` (workflow) embeds it" **cannot compile**, and `status` is **two
  response shapes** (rich `BootstrapResponse` JSON vs markdown) the `Lifecycle` struct can't carry.
  **Clean resolution:** `ResolveLifecycle` owns **only the precedence decision** (`focus =
  infra|work|idle`); the **dispatcher** routes `focus=infra` to the existing rich bootstrap handler and
  `focus=work/idle` to the envelope/markdown path; launch is injected at the dispatcher **after**
  `ComputeEnvelope`. This is the *actual* SPINE-1 fix (unify precedence) without collapsing shapes or
  violating layering.
- **MAJOR:** failed/partial deploys poison the footprint (success-gate membership on first SUCCESSFUL
  deploy — mirror `stampFirstDeployedAt` `work_session.go:220` — while still recording all attempts).
- **MAJOR/GAP:** `iteration-cap` close (`closeWorkSessionOnCap`) persists `ClosedAt` on a **non-green**
  footprint (a failure-STOP) — not footprint-derivable. Surface **`CloseReason`** in `Lifecycle`:
  derive `auto-complete`; keep `explicit` + `iteration-cap` as the two PERSISTED-`ClosedAt` cases.
- **MAJOR/RISK:** raw-disk `ClosedAt==""` readers go **stuck-open** under pure-derive — `guard.go:49`,
  `server/instructions.go:104`, `workflow_develop.go:187`, `deploy_local.go:259`, `compute_envelope.go:321/403`,
  `render.go:50`. Rewire all to a single derived `IsOpen` predicate **before** deleting
  `MaybeFireAutoClose`; then lint-forbid raw `ws.ClosedAt` reads outside it.
- **MINOR (factual):** the packet says explicit close "persists `ClosedAt`" — today explicit close
  **DELETES the file** (`workflow.go:1138`); `CloseReasonExplicit/Abandoned` are dead constants.
  Persisting it is a small NEW addition, not a retention.
- **MINOR:** `git-push-setup` is the ONE RMW site with SSH + env-write + a polled container RESTART
  between read and write (`workflow_git_push_setup.go:88→432`) — cannot wrap in an `Update` closure
  holding the flock across network I/O without violating the design's own §9 exception. Specify
  *decide-outside-lock / commit-field-delta-inside* for this site only; the other ~14 are call-site
  substitutions.
- **TEST:** the promised "two concurrent processes start bootstrap" pin **does not exist**; current
  coverage is in-process goroutines (a `sync.Mutex` swap would pass it). Land a **fork-two-OS-processes**
  test — now as a **per-hostname** acceptance criterion (Blocker 1), plus a cross-process dead-PID-reclaim
  test.

---

## 6. The simplified target (what it becomes after the cuts)

**Three stores, one lock primitive, one resolver, derived auto-complete:**
- **`services/{hostname}.json`** (ServiceMeta, project) — RMW via the lifted `withFileLock`.
- **registry** (project) — the single lifecycle authority: add `StartTime` to `SessionEntry`; the
  **per-hostname** bootstrap-exclusivity check goes **inside `InitSessionAtomic`** (under the flock it
  already holds — the place the phantom comment already promises). Dead-PID reclaim via `(pid,startTime)`
  + the existing TTL prune.
- **`work/{pid}-{startTime}.json`** (per-PID develop) — keyed by `(pid,startTime)`.
- **launch** stays project-scoped, rendered as a uniform project element (no overlay noun).
- **`ResolveLifecycle(stateDir, pid)`** owns precedence only (reads disk, not `e.sessionID`); dispatcher
  routes the two response shapes; derives `auto-complete` over DECLARED scope; surfaces `CloseReason`.

**Net:** the design's CONCEPT is right. Cut the project-singleton + its takeover apparatus + the
mutex-map + generic `Update[T]` packaging + I11 + the "overlay" noun + the cross-user-EPERM rationale;
fix the two close/scope fixpoints and the layering. That is a clean architecture without the
imagined-threat machinery.

## 7. Build order (revised)
1. **R7** — reconcile the spec (focus rule; **decide Blocker 1**); strike the wrong load-bearing comments
   (`engine.go:19-31` is right / keep; `work_session.go:90-91` is wrong / delete; `session.go:181` phantom / fix).
2. **XCUT-1** — lift `withFileLock`; route the 15 ServiceMeta RMW callers through it (git-push-setup gets
   the decide-outside-lock pattern); add the AST-lint.
3. **SPINE-4** — make `checkHostnameLocks` pair-aware (`FindServiceMeta`); add the **per-hostname**
   two-OS-process pin.
4. **SPINE-1** — `ResolveLifecycle` (precedence-only, disk-only); remove `e.sessionID` as the status
   precedence source; dispatcher routes shapes; demote launch from the preempt.
5. **Close/scope** — declared-scope denominator + additive success-gated footprint; derive auto-complete,
   persist explicit + iteration-cap with `CloseReason`; rewire all `ClosedAt` readers to one `IsOpen`;
   delete `MaybeFireAutoClose`.
6. **`(pid,startTime)`** for both stores; two-state liveness + TTL backstop.

*Per repo policy every new invariant lands with a pinning test — and the per-hostname two-process test is
the one whose absence let SPINE-4 hide.*
