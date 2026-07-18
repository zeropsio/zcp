# ZCP Session & Lifecycle Rebuild — v4 spec + implementation plan (2026-05-29)

**Decision (owner): per-hostname exclusivity, SIMPLIFY-THEN-SHIP.** This v4 folds the independent
review verdict (`plans/zcp-session-lifecycle-REVIEW-VERDICT-2026-05-29.md`) into the design and is the
spec + phased plan the implementation is written against. It supersedes v3
(`plans/zcp-session-lifecycle-architecture-2026-05-29.md`) and the review packet.

The verdict confirmed the problem (8/8) and the core concept, but found the v3 machinery ~1/3
over-built — most of it cascading from a project-singleton bootstrap that **contradicted the pinned
per-hostname contract** (`TestEngine_BootstrapParallelAllowed`, spec §2.2 "not global"). v4 keeps
per-hostname (prevents the real corruption AND preserves parallel different-service bootstraps across
tabs) and cuts the cascade.

---

## 1. The simplified target (what we build)

**Three stores, one lock primitive, one precedence resolver, derived auto-complete.**

| Store | Scope | Lock | Identity |
|---|---|---|---|
| `services/{hostname}.json` (ServiceMeta) | project | lifted `withFileLock` (flock) | hostname |
| **registry** (the single lifecycle authority) | project | existing `withRegistryLock` (flock) | `(pid, startTime)` |
| `work/{pid}-{startTime}.json` (develop) | per-PID | `withFileLock` | `(pid, startTime)` |
| `launch-production/{launchID}.json` | project (overlay) | `withFileLock` | launchID |

- **No new `sessions/infra-lock.json`** — the registry is already the project-scoped, flock-guarded,
  PID+workflow+project store with dead-PID auto-claim. Adding a 4th store is the very thing R1 warns
  against. Bootstrap stays a registry session; per-hostname exclusivity goes **inside
  `InitSessionAtomic`**, under the flock it already holds.
- **No operator-CAS-takeover apparatus, no tri-state liveness, no generic `state.Update[T]`
  package** — all existed to defend the project-singleton; with per-hostname they dissolve.
- **`withFileLock(path, fn)`** lifted out of `registry.go` is the ONE lock primitive; work-write,
  hostname-claim, and ServiceMeta-RMW are three callers of it.

### The focus rule (R7 / SPINE-1 fix) — unchanged, correct
```
focus(pid) = operatesInfra(pid) ? infra   // driving a bootstrap → foreground
           : hasWork(pid)       ? work     // your develop task (open or suspended)
           : idle
```
Spec §5.3 ("bootstrap PRIMARY") and §6.2 ("work-first") are two *focus states* of one machine, not
contradictory rules. `ResolveLifecycle` computes this from DISK (never in-memory `e.sessionID`).

### develop ⇄ bootstrap nesting (requirement #1) — does NOT need a project lock
A PID may hold a develop work session AND operate a per-hostname bootstrap. Entering bootstrap
**suspends** the work (never closes it); closing bootstrap resumes it. Per-hostname exclusivity is
orthogonal to nesting.

### Parallel sessions (requirement #2)
N PIDs (Claude tabs, Codex, Antigravity) each hold their own develop work; cross-process flock makes
ServiceMeta + registry writes safe; bootstraps of **different** services proceed in parallel, **same**
service is locked (per-hostname). This satisfies #2 better than a project-singleton (which would
serialise different-service bootstraps across tabs).

---

## 2. Auto-complete & scope (derived, not an event)

- **Declared scope stays the auto-close denominator** (the verdict's Blocker 2: a deployed-so-far
  footprint fires premature close on the first green service). `footprint = union(declared,
  successfully-deployed)` — the union is the DEV-1 fix (a mid-task deploy of a new runtime is tracked,
  not rejected), but the close test is **all of DECLARED green**.
- **`develop-closed-auto` is DERIVED**, computed by `ResolveLifecycle`:
  ```
  phase = ws closed by explicit/iteration-cap (CloseReason persisted) → develop-closed
        : all DECLARED green && !operatesInfra(pid)                    → develop-closed-auto (DERIVED)
        : work present                                                 → develop-active
        : else                                                         → idle
  ```
- **`MaybeFireAutoClose` is deleted.** The phase is a pure function of state, identical on every
  surface (status, deploy response, envelope) — the gate cannot desync (the audit's DEV-2/lazy-fire
  class dissolves). Compaction-safe (pure over disk).
- **`CloseReason`** is surfaced in the lifecycle: `auto-complete` is DERIVED (never persisted);
  `explicit` and `iteration-cap` (failure-STOP on a NON-green footprint, not derivable) are the two
  PERSISTED-`ClosedAt` cases.
- **Membership is success-gated**: a runtime joins the footprint on its first SUCCESSFUL deploy
  (mirror `stampFirstDeployedAt`); failed/partial deploys are recorded as attempts but don't poison
  the footprint.
- **One `IsOpen(ws)` predicate** replaces every raw `ws.ClosedAt == ""` read; raw reads lint-forbidden.

---

## 3. ResolveLifecycle — precedence-only (layering-safe)

`launch_state.go` is `package tools` (layer 4); `.golangci.yaml` forbids `workflow/`→`tools/`. So a
`workflow`-side resolver CANNOT embed launch. Resolution:
- `ResolveLifecycle(stateDir, pid) → focus ∈ {infra, work, idle}` lives in `workflow/`, reads DISK
  (registry + work file + service metas), owns ONLY the precedence decision + the derived phase.
- The **dispatcher** (`tools/workflow.go`) routes: `focus=infra` → the existing rich bootstrap status
  handler; `focus=work|idle` → the envelope/markdown path. **Launch is injected at the dispatcher,
  AFTER `ComputeEnvelope`** (tools layer can see both) — demoting it from today's status preempt
  (`workflow.go:453`) so it no longer hides develop; rendered as one uniform project element.
- This is the real SPINE-1 fix (unify precedence, remove `e.sessionID` as the source) without
  collapsing the two response shapes or violating layering.

---

## 4. Identity & liveness (two-state)

- `(pid, startTime)` for the operator (registry `SessionEntry`) AND the work session
  (`work/{pid}-{startTime}.json`).
- Liveness = `isProcessAlive(pid) AND startTime matches` → **two-state (alive / dead)**. The v3
  tri-state `unknown` branch was built on a false premise: start-time IS world-readable (Darwin
  `SysctlKinfoProc`, Linux `/proc/<pid>/stat` field 22) — verify at impl, but no `unknown` case is
  expected. Backstop: the existing 24h `pruneDeadSessions` TTL.
- A recycled PID has a newer start-time → classified dead → correctly reclaimable; never a false-live
  wedge.

---

## 5. Invariants (v4 — simplified)

| # | Invariant | Enforcement |
|---|---|---|
| I1 | ≤1 work session per PID | single `work/{pid}-{startTime}.json` |
| I2 | bootstrap exclusivity is PER-HOSTNAME | the real check inside `InitSessionAtomic` under the registry flock; same-hostname incomplete-meta-from-alive-session blocks; dead PID → auto-unlock. (Matches pinned `TestEngine_BootstrapParallelAllowed` + spec §2.2.) |
| I3 | hostname→meta lookups are pair-aware | `checkHostnameLocks` uses `FindServiceMeta`, never `ReadServiceMeta(hostname)` (catches stage-half collision = SPINE-4) |
| I4 | "what is primary" has one answer | one `ResolveLifecycle` (disk-only), consumed by dispatcher + envelope (SPINE-1) |
| I5 | ServiceMeta RMW never loses updates | all ~15 callers route through one `withFileLock`-guarded update; git-push-setup uses decide-outside-lock; raw WriteServiceMeta-after-read lint-forbidden (XCUT-1) |
| I6 | a PID may operate infra while holding work (nesting) | work → suspended on infra-enter, resumed on close |
| I7 | auto-complete is derived, never an event | `ResolveLifecycle` computes `develop-closed-auto`; `MaybeFireAutoClose` deleted; one `IsOpen` predicate; explicit + iteration-cap persist `ClosedAt` + `CloseReason` |
| I8 | declared scope is the close denominator; footprint is additive + success-gated | union(declared, succeeded); close = all DECLARED green |
| I9 | identity is `(pid,startTime)`, liveness two-state | registry + work keyed by it; +24h TTL backstop |
| I10 | launch is project-scoped, recoverable by any PID, demoted from preempt | `launch-production/{launchID}.json`; injected at dispatcher post-envelope |
| I11 | clean migration, no mixed-fleet bridge | no silent auto-update ⇒ reopen-on-update; persistent state self-heals; ephemeral sessions restarted |

(Dropped vs v3: project-singleton I2, operator-CAS, tri-state, the recipe-mutual-exclusion I11 — the
e.Start recipe path is dead; live recipe is the separate `zerops_recipe`/`recipe.NewStore`, untouched.)

---

## 6. Phased implementation plan (build order + gates + fidelity checklist)

Each phase: RED (test) → GREEN (impl) → GATE (build+test). No phase advances until its gate is green.
Per-item status tracked here (CLAUDE.md plan-fidelity: no silent scope cuts).

### P1 — R7: reconcile the spec (doc-only)
- [ ] `docs/spec-work-session.md` §5.3/§6.2/§7.4 → the focus rule (bootstrap foregrounds work; one resolver). Strike §6.2 "work-first" routing precedence.
- [ ] `docs/spec-workflows.md` §2.2/B6 → keep per-hostname; name recipe's real entry (`zerops_recipe`).
- [ ] Comment fixes: KEEP `engine.go:19-31`; DELETE/fix wrong `work_session.go:~90-91`; fix phantom `session.go:~181` (to match the P3 real check).
- **Gate:** docs only; `go build ./...` unaffected. No behavior change.

### P2 — XCUT-1: single locked ServiceMeta write path
- [ ] Lift `withFileLock(path, fn)` out of `registry.go` (the existing flock helper) into a reusable form.
- [ ] Route ALL ServiceMeta RMW callers (recon inventory; ~15) through a locked read-modify-write.
- [ ] git-push-setup special-case: decide-outside-lock, commit field-delta inside (no flock across SSH/restart I/O).
- [ ] AST-lint test forbidding raw `WriteServiceMeta` after a `ReadServiceMeta`/`FindServiceMeta` in the same fn (topology arch-test pattern).
- [ ] Document the `CLAUDE.md:404` exception (RMW transaction holds lock across bounded local-file I/O).
- **Gate:** `go test ./internal/... -race`; new lint test RED→GREEN; existing meta tests green.

### P3 — SPINE-4: pair-aware per-hostname lock
- [ ] `checkHostnameLocks`: `ReadServiceMeta` → `FindServiceMeta` (pair-aware).
- [ ] Implement the REAL per-hostname exclusivity check inside `InitSessionAtomic` (under the flock); fix the phantom comment.
- [ ] Add a fork-two-OS-processes per-hostname concurrency pin (current pins are in-process goroutines).
- **Gate:** `TestEngine_BootstrapParallelAllowed` + `TestInitSessionAtomic_*` + `TestEngine_BootstrapExclusivity_DeadPID` stay green; new cross-process pin RED→GREEN; `-race`.

### P4 — SPINE-1: one precedence resolver
- [ ] `ResolveLifecycle(stateDir, pid)` in `workflow/` — precedence-only (focus enum + derived phase), DISK-only.
- [ ] Remove `e.sessionID` as the status precedence source; dispatcher routes the two response shapes via the resolver.
- [ ] Demote launch from the status preempt; inject post-`ComputeEnvelope` at the dispatcher.
- [ ] `ComputeEnvelope` consumes the same `ResolveLifecycle`.
- **Gate:** new concurrent bootstrap+develop status test RED→GREEN; existing status/envelope tests green; layering depguard passes; `-race`.

### P5 — close/scope: derived auto-complete + dynamic footprint
- [ ] One `IsOpen(ws)` predicate; rewire ALL `ClosedAt` readers (recon inventory) to it BEFORE deleting `MaybeFireAutoClose`.
- [ ] Derive `develop-closed-auto` (all DECLARED green && !operatesInfra) in `ResolveLifecycle`; surface `CloseReason`.
- [ ] `RecordDeployAttempt`: add successfully-deployed runtime to footprint instead of rejecting out-of-scope; keep declared as denominator.
- [ ] Persist `ClosedAt`+`CloseReason` for explicit + iteration-cap close (note: explicit today DELETES the file — this is a small new addition).
- [ ] Delete `MaybeFireAutoClose` + lint-forbid raw `ws.ClosedAt`.
- [ ] Regenerate affected goldens + `scenarios_test.go` pins.
- **Gate:** golden/scenario diffs reviewed; `go test ./internal/... -race`; declared-scope-not-prematurely-closed pin (`scope=[web,api]`, deploy web only → still active).

### P6 — (pid,startTime) identity + two-state liveness
- [ ] `StartTime` on registry `SessionEntry`; work session keyed `work/{pid}-{startTime}.json`.
- [ ] Liveness = alive AND startTime-match (two-state) + 24h TTL backstop; `CleanStaleWorkSessions` validates startTime.
- [ ] Cross-process dead-PID-reclaim test.
- **Gate:** `go test ./internal/... -race`; PID-reuse pin (recycled PID ⇒ dead ⇒ reclaimable).

### P7 — review + full suite
- [ ] `go test ./... -race -count=1`; `make lint-local`.
- [ ] Adversarial multi-agent review of the diff (correctness, layering, v4-fidelity, no orphans, no half-finished states); fix findings.

### P8 — container flow-eval (end-to-end behavioral)
- [ ] Build + deploy to eval-zcp (container flow-eval; NO local `make install`).
- [ ] Scenarios: concurrent bootstrap+develop `status`; develop→bootstrap→resume; derived auto-complete; per-hostname parallel bootstrap (two services); dead-PID reclaim.
- [ ] Read `self-review.md`; analyze friction → atom/spec/code edits.

---

## 7. What was CUT (vs v3) + why
| Cut | Reason |
|---|---|
| project-singleton bootstrap (I2) | contradicts pinned per-hostname contract (`TestEngine_BootstrapParallelAllowed`, spec §2.2 "not global"); over-serialises parallel tabs |
| `sessions/infra-lock.json` + new lock primitive | registry already is the project flock store (R1: don't add a 4th); lift `withFileLock` instead |
| operator-CAS-takeover | existed for the singleton; per-hostname lock + dead-PID prune suffice |
| tri-state liveness (`unknown⇒refuse`) | false premise — start-time IS world-readable; two-state + TTL |
| generic `state.Update[T]` + `internal/workflow/state` package | over-abstraction; lock ServiceMeta directly via `withFileLock` |
| in-process mutex-map | flock serialises same-process too (registry precedent); add only if profiling shows contention |
| I11 bootstrap↔recipe mutual-exclusion | the e.Start recipe path is DEAD; live recipe is separate (`zerops_recipe`) — nothing to preserve |
| "LaunchOverlay" reified noun / FOCUS-STORAGE as carried structs | a lens, not data; launch = one uniform project element |

---

## 8. Constraints honored
- **No `make install` / `make release`** without explicit ask. Eval = **container** flow-eval (builds +
  ssh-deploys to eval-zcp; does not touch local `/usr/local/bin`).
- **Published-product backward-compat:** ServiceMeta normalize-on-read; clean migration, no bridge
  (no silent auto-update → reopen-on-update); keep old action strings/fields accepted.
- **TDD** RED→GREEN per phase; every new invariant pinned (the missing per-hostname cross-process test
  is the one whose absence hid SPINE-4).
- **Layering** (`.golangci.yaml` depguard): `workflow/` must not import `tools/` — ResolveLifecycle is
  precedence-only; launch injected at the dispatcher.

---

*Implementation proceeds phase-by-phase off the recon change-site inventory (workflow `w5e6hpken`).
Per-phase fidelity reported against §6 before advancing.*
