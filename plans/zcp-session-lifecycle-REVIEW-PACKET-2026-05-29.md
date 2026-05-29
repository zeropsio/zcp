# ZCP Session & Lifecycle Architecture — Independent Review Packet (2026-05-29)

**This file is self-contained.** It bundles everything an independent reviewer needs to review and
verify the proposed session/lifecycle architecture without the originating conversation or other
files: the problem (with file:line evidence), the complete design, the validation already performed,
the decisions, and a concrete verification checklist. It consolidates and supersedes the working doc
`plans/zcp-session-lifecycle-architecture-2026-05-29.md` (v3).

---

## 0. For the reviewer — how to use this packet

- **Repo:** `github.com/zeropsio/zcp` (the working copy you have). Go. A single binary: MCP server +
  CLI for the Zerops PaaS.
- **What this proposes:** a greenfield model for ZCP's *session & lifecycle* layer, to replace an
  incoherent one that causes data corruption and wrong post-compaction recovery. It is a DESIGN, not
  yet implemented.
- **What we want from you:** (1) confirm the PROBLEM is real (§1 gives file:line evidence — spot-check
  it); (2) judge whether the DESIGN (§2–§7) is correct, complete, and the best approach; (3) find
  holes our two prior adversarial passes (§8) missed; (4) sanity-check implementability (§10) and the
  decisions (§9).
- **What's already been done** (so you go beyond, not redo): a 3-audit root-cause analysis, a 6-angle
  adversarial "break-test", and two independent Codex code-reviews. §8 records what each found,
  refuted, and corrected. The design below is the result *after* those passes.
- **Authority order in this repo** (from `CLAUDE.md`): tests > code > specs > plans. Where this design
  cites code, verify against the code; where it cites the spec, note the spec itself is internally
  contradictory (that's finding R7).

---

## 1. The problem (verifiable — spot-check the file:line)

### 1.1 What ZCP is
A state machine that derives AI-agent guidance from persisted evidence under `.zcp/state/`. Workflows:
**bootstrap** (provision Zerops services) → **adopt** → **develop** (a per-PID "work session":
deploy/verify/iterate) → **export** → **launch-production** (promote dev/stage into a NEW prod
project). Published product: users have it installed with on-disk `.zcp/state`. The owner runs MANY
parallel agents — multiple Claude tabs/terminals AND other AI CLIs (Codex, Antigravity) — often over
ONE shared `.zcp/state`. **Cross-process, cross-tool concurrency is the norm.**

### 1.2 Root cause (from `plans/zcp-architecture-rootcause-audit-2026-05-29.md`)
Three independent audits converged on the same finding: **ZCP has no coherent session/lifecycle
model.** Two genuinely different session kinds (per-agent *work* vs per-project *infra mutation*) were
collapsed into one undifferentiated "session" with a single scalar `Phase`, resolved in two places
with **opposite precedence**, and the central state object (`ServiceMeta`) is mutated read-whole →
write-whole with **no lock** while the SDK dispatches tool calls concurrently.

### 1.3 The specific defects this design fixes — each verifiable against code
| ID | Defect | Evidence (verify) |
|---|---|---|
| **SPINE-1** | "what workflow is active / what is primary" computed twice with **opposite** precedence; `action=status` (the canonical post-compaction recovery primitive) takes the wrong branch in a concurrent bootstrap+develop session | dispatcher infra-first: `internal/tools/workflow.go:441-467` (`detectActiveWorkflow` → `handleBootstrapStatus`, never reads the work session); envelope work-first: `internal/workflow/compute_envelope.go:398-411` (`derivePhase`) |
| **SPINE-3** | `claimSession` saves the state-file PID then **discards** the registry-PID update error → the two stores disagree on ownership | `internal/workflow/engine.go:141-149` (`_ = updateRegistryPID(...)`) |
| **SPINE-4** | concurrent bootstraps corrupt shared state; the only "exclusivity" check is a phantom | `internal/workflow/session.go:181` doc-comment claims a bootstrap-exclusivity check; body `:203-227` does none; `atomicWriteJSON` = `os.Rename` (overwrites) `:168`; hostname-lock uses non-pair-aware `ReadServiceMeta` at `engine.go:726` |
| **XCUT-1** | the three "orthogonal deploy dimensions" are concurrent **unsynchronized read-modify-write** on one JSON file → lost orthogonal writes under parallel tool_use | `WriteServiceMeta` unlocked: `internal/workflow/service_meta.go:365-386`; RMW handlers: `workflow_close_mode.go:207`, `workflow_git_push_setup.go:309`, `workflow_build_integration.go:198`; SDK concurrency: go-sdk `mcp/server.go:1441` (`jsonrpc2.Async`) |
| **DEV-1** | a mid-task deploy of a service outside the fixed scope is **silently rejected/untracked** | `internal/workflow/work_session.go:201` (`ErrHostnameOutOfScope`) |
| **PID-reuse** | operator/agent liveness is bare `syscall.Kill(pid,0)` (true on `EPERM`) → a recycled PID reads "alive" → singleton can wedge forever | `internal/workflow/registry_unix.go:45-51` |
| **launch coupling** | launch state is project-scoped + PID-agnostic, but the dispatcher's `status` preempts develop with launch recovery (hides develop) | `internal/tools/launch_state.go:18-58,121-133,218-266` (no PID field; launchID=hash; PID-agnostic recovery); dispatcher preempt: `internal/tools/workflow.go:453` |
| **R7 spec contradiction** | the spec the code follows contradicts itself on lifecycle precedence — the actual cause of SPINE-1 | `docs/spec-work-session.md` §5.3/§9.3 (bootstrap-PRIMARY) vs §6.2 ("If work session exists → return Work Session status", work-first) |

### 1.4 The two hard requirements (judge fit-to-purpose)
1. **develop ⇄ bootstrap nesting is first-class** — an agent in develop steps into a bootstrap ("add
   redis" mid-task) and returns to its develop work afterward, by design.
2. **Many parallel sessions (cross-tab AND cross-tool), but bootstrap is a singleton** — ≤1 active
   bootstrap per project until it closes.

---

## 2. The design — core model: two axes (FOCUS, STORAGE)

The key separation: **"what an agent is doing right now" (focus) is independent of "where state lives
and who recovers it" (storage).** Conflating them was the central legacy bug.

### Axis A — FOCUS (per PID): what is *primary* for this agent now
Exactly two focusable slots:

| Slot | Kind | Cardinality | Nests? |
|---|---|---|---|
| **Work** | `develop` | 1 per PID | — |
| **Infra-op** | `bootstrap` | ≤1 per project (the lock) | **yes — nests over work** |

**The focus rule** (resolves "what is primary"; fixes SPINE-1):
```
focus(pid) = operatesInfraOp(pid) ? infra-op   // you're driving the bootstrap → foreground
           : hasWork(pid)         ? work        // your develop task (open or suspended)
           : idle
```
> spec §5.3 ("bootstrap PRIMARY, work backgrounded") and §6.2 ("work-first") were never contradictory
> *rules* — they are two *focus states* of one machine. Operating the bootstrap ⇒ `focus=infra-op`;
> it closes ⇒ focus falls back to the still-alive work. One rule, one resolver, zero ambiguity.

### Axis B — STORAGE / recovery scope
| Kind | Storage | Recoverable by | Reaped on PID death? |
|---|---|---|---|
| `develop` (work) | per-PID `agents/{pid}-{startTime}.json` | the owning PID | yes (stale-PID cleanup) |
| `bootstrap` (infra-op) | project `sessions/infra-lock.json` | any live PID (operator died) | **no — survives, reclaimable** |
| `launch` **(overlay)** | project `launch-production/{launchID}.json` | **any live PID on the project** | **no — survives** |

### Launch is a project OVERLAY, not a focus slot
Launch's *concurrency property* is "work-like" (per-agent-initiated, parallel, no source-project lock),
but its *storage/recovery* is project-scoped (the async Zerops launch pipeline outlives the process;
the one-shot launchKey is burned + never persisted; recovery today is PID-agnostic). It therefore does
NOT occupy the per-PID focus slot:
- **Generic `status`:** the PID's own focus (develop/bootstrap) is primary; an active launch renders as
  an **overlay note** ("⟳ production launch in progress on this project: …"), visible to every PID — it
  no longer hides develop (today it does, `workflow.go:453`).
- **Explicit launch calls:** the handler returns a launch-focused response.
- **No work + active launch:** the launch overlay becomes the natural primary.

### Recipe is out of scope for THIS model
`recipe-authoring` is a separate `zerops_recipe` tool (today `zerops_workflow` rejects recipe and
points there — `workflow.go:549`) and shares the single engine session slot with bootstrap
(`engine_recipe.go` → `e.Start`, which refuses a second active session — `engine.go:157-170`). The
only contract this design asserts: **bootstrap and recipe-authoring are mutually exclusive** (one
infra/authoring session per project). Full integration is deferred to its owner; this design must not
break the mutual exclusion.

**Operator** = the PID holding the infra-op lock, identified by **`(operatorPID, operatorStartTime)`**
— never a bare PID.

---

## 3. Persisted state layout

```
.zcp/state/
  version                          ← schema version sentinel (gates one-way migration; see §10)
  services/{hostname}.json         ← ServiceMeta (project evidence; storage unchanged)
  sessions/infra-lock.json         ← THE active infra-op (project-exclusive). bootstrap only.
                                       Presence == the lock. { kind:bootstrap, operatorPID,
                                       operatorStartTime, route, step, startedAt }
  agents/{pid}-{startTime}.json    ← per-PID work (develop), keyed/validated by (pid,startTime)
                                       { work:{ state:open|suspended|closed, footprint[], intent,
                                                deploys{}, verifies{}, closedAt? } }
  launch-production/{launchID}.json ← launch OVERLAY — project-scoped, launchID=hash(srcProj::tgtProj),
                                       any-PID-recoverable. NOT per-PID.
  registry                         ← flock-guarded: live (pid,startTime) + liveness for reclaim
```
- **Project-scoped** (shared across PIDs; every write flock-guarded): `services/`,
  `sessions/infra-lock.json`, `launch-production/`, `registry`, `version`.
- **Per-PID:** `agents/{pid}-{startTime}.json` — keyed by `(pid,startTime)` so a recycled PID can't
  inherit a dead agent's work; stale-cleanup validates start-time too.

**Operator/agent liveness is TRI-STATE** (not the bare `Kill(pid,0)` which reads `EPERM` as alive —
`registry_unix.go:45`). Stamp `(pid, startTime)` (Darwin: `unix.SysctlKinfoProc` →
`ExternProc.P_starttime`; Linux container: `/proc/<pid>/stat` field 22). Classify:
- **alive** = pid exists AND start-time matches → operator valid; refuse takeover.
- **dead** = pid gone, OR start-time mismatch (PID reused) → reclaimable.
- **unknown** = can't read start-time (e.g. `EPERM`, cross-user) → **REFUSE takeover** with a
  diagnostic (never steal from a possibly-live operator, never wedge silently).

**The lock primitive:** claim `infra-lock.json` by **read-presence-then-create inside one held project
flock** (absence-test + create share one critical section). NOT `os.Rename`-as-create (`session.go:168`
overwrites → last-writer-wins → both think they hold it). Pin with a two-concurrent-process
`start bootstrap` test asserting exactly one operator + exactly one non-refused start.

---

## 4. `ResolveLifecycle` — a lifecycle OVERLAY, not a replacement for `ComputeEnvelope`

`ResolveLifecycle` resolves ONLY the lifecycle/focus/phase layer; `ComputeEnvelope` still computes
services/snapshots/etc. (`envelope.go:19`) and **embeds** the lifecycle result. Both the dispatcher
`status` path and the envelope consume the SAME `ResolveLifecycle` (killing the two-precedence SPINE-1
split — today dispatcher reads in-memory `e.sessionID` at `workflow.go:441`, envelope reads disk at
`derivePhase` `compute_envelope.go:398`).

```go
type WorkState int   // Open | Suspended | Closed
type Lifecycle struct {
    Focus         FocusRef       // Infra | Work | Idle — the actionable primary (§2 rule)
    Work          *WorkSummary   // this PID's develop (state: open|suspended|closed)
    ProjectInfra  *InfraSummary  // active bootstrap? operator (pid,startTime) + tri-state liveness
    LaunchOverlay *LaunchSummary // project launch in flight (any PID) — overlay, not focus
    Phase         Phase          // deterministic mapping, for atom filtering + BuildPlan
}
func ResolveLifecycle(stateDir string, pid int) Lifecycle  // consumed by dispatcher AND ComputeEnvelope
```
- A `Closed` work resolves to idle-with-resume-offer — never a phantom foreground.
- `Phase` is a pure function of the lifecycle, preserving the existing atom-axis/BuildPlan contract.
- **Implementation note:** bootstrap/recipe status today go through `engine.BootstrapStatus()` via
  in-memory `e.sessionID` (`workflow_bootstrap.go:140`); these need session-id-addressed loaders — a
  real refactor, not a drop-in.

---

## 5. Scope & close — dynamic footprint + forced auto-close as a DERIVED phase

**Dynamic footprint:** a develop session's scope = the runtime services it actually deploys. Deploying
a runtime **adds it to the footprint** (managed deps excluded); no upfront declaration. This requires
changing the recorder: today `RecordDeployAttempt` REJECTS out-of-scope hostnames
(`ErrHostnameOutOfScope`, `work_session.go:201`) — instead it must **add the deployed runtime to the
footprint**. Kills DEV-1 (nothing is out-of-scope).

**Forced auto-close stays** (owner principle: workflows must be invisible — no explicit "close" step
the system can infer) — but implemented as a **DERIVED phase, not a persisted event**. This is the
clean realization that also kills the legacy auto-close bug class (gate-desync, lazy-fire from every
surface, status-doesn't-fire-gate). The phase is *computed* by `ResolveLifecycle`:
```
phase = work.closedAt set                          → develop-closed (explicit close)
      : footprint all-green && !operatesInfraOp     → develop-closed-auto   (DERIVED — invisible)
      : work present                                → develop-active
      : else                                        → idle
```
- **No `closeSuggestion`, no fire/retract event, no per-surface `MaybeFireAutoClose`** — the phase is
  a pure function of the footprint, identical everywhere, so the gate cannot desync. Net blast radius
  *shrinks* vs the legacy event-based close: delete `MaybeFireAutoClose`, derive instead.
  (Compaction-safe: pure over state.)
- **Invisible re-engagement:** a derived close auto-reverts the moment a new deploy makes the footprint
  non-green; a deploy after a derived close just continues — no explicit reopen.
- **Excursion safety:** while `operatesInfraOp(pid)` the work is **suspended** — `develop-closed-auto`
  is NOT computed for it (the `!operatesInfraOp` conjunct). On `close bootstrap` it's re-derived once.
  The legacy "excursion resumes a dead session" hazard is impossible: nothing is persisted-closed
  behind the excursion.
- **Explicit close** (agent/user calls `close`) still persists `closedAt`.

---

## 6. Transitions — the complete state machine

| Verb | Precondition | Effect |
|---|---|---|
| `start develop` | no work for (pid,startTime) | create work (open); focus → work |
| `start develop` (force) | work exists, different intent | force-discard prior per existing rule |
| **`start bootstrap`** | `infra-lock.json` absent | claim via flock-critical-section create; operator=(pid,startTime); focus → infra; work → **suspended** |
| `start bootstrap` | present, operator **alive** ≠ me | **REFUSE** — "bootstrap active (operator PID X)" |
| `start bootstrap` | present, operator **dead** (gone / start-time mismatch) | **CAS takeover** under flock (no discarded errors — fixes SPINE-3); focus → infra |
| `start bootstrap` | present, operator **unknown** (can't read start-time) | **REFUSE + diagnostic** — never steal from a maybe-live operator |
| `start bootstrap` | present, operator = me | idempotent |
| `complete`/`skip` step | operator = me | advance `step` |
| **`close bootstrap`** | operator = me | release `infra-lock.json`; work `suspended → open`; phase **re-derived** (footprint all-green → develop-closed-auto, invisibly); focus → work or idle |
| `close develop` | work present, NOT operating infra | mark work closed; focus → idle |
| `close develop` | operating infra | **REFUSE** — "close the bootstrap excursion first" (top-down) |
| `reset` | any | mark this PID's work closed; if operator, release `infra-lock.json`; focus → idle (fixes SPINE-2) |
| `start launch` / continue | — | overlay op (does NOT touch the focus slot); launch-focused response; state in `launch-production/{launchID}.json` |
| `close`/terminal launch | — | terminal-state the launch overlay (project-scoped); focus unchanged |
| `status` | any | **read-only** `ResolveLifecycle` → render `focus` primary + launch overlay + project-infra awareness. Dead-operator reclaim happens on the next **`start bootstrap`**, NOT here |

With launch as an overlay, the only focus-main is `develop`; `start launch`/`start bootstrap` don't
compete for it. `status` is purely read-only; the only state-mutating recovery verb is `start
bootstrap` (CAS takeover).

**Required flow:**
```
start develop                  → focus=work; footprint grows as you deploy
start bootstrap "add redis"    → claim infra-lock; focus=infra; work SUSPENDED (auto-close paused)
close bootstrap                → release; work OPEN; phase re-derived → "redis ready · web,api auto-complete"
```

---

## 7. Invariants & edge cases

### Invariants (each must be enforced by something concrete, not by convention)
| # | Invariant | Enforcement |
|---|---|---|
| **I1** | ≤1 work session per PID | single work record in `agents/{pid}-{startTime}.json` |
| **I2** | ≤1 active infra-op per project | `infra-lock.json` via read-presence-then-create inside one flock; `os.Rename`-as-create forbidden |
| **I3** | a PID may operate infra while holding work (nesting) | `operatesInfraOp` + focus rule; work → suspended (never closed) on infra-enter |
| **I4** | "what is primary" has one answer | one `ResolveLifecycle`, consumed by dispatcher AND envelope |
| **I5** | infra-op survives operator death; reclaimable, never wedged, never stolen-from-live | `(pid,startTime)` identity + tri-state liveness; unknown → refuse; reclaim = CAS under flock, no discarded errors |
| **I6** | concurrent writes never lose updates | all RMW behind `state.Update[T]` — **every** site (close-mode, git-push-setup, build-integration, first-deploy-stamp, adopt). Holds a lock across local-file I/O → a sanctioned, documented exception to `CLAUDE.md:404` (see §9) |
| **I7** | backgrounded work is suspended; the excursion returns to a LIVE session | suspend on infra-enter; `develop-closed-auto` NOT computed while suspended; re-derived once on close bootstrap |
| **I8** | auto-close is derived, never an event (no gate-desync) | `ResolveLifecycle` computes `develop-closed-auto` from footprint-all-green; `MaybeFireAutoClose` deleted; explicit `close` persists `closedAt` |
| **I9** | launch state is project-scoped, any-PID-recoverable, never PID-reaped, rendered as overlay | `launch-production/{launchID}.json`; project-keyed; excluded from stale-PID cleanup; not a focus slot |
| **I10** | upgrades migrate persistent state once, losslessly; ephemeral session state is restarted clean | `version` sentinel + normalize-on-read; no mixed-fleet bridge (§10) |
| **I11** | bootstrap and recipe-authoring are mutually exclusive | one infra/authoring engine session per project (preserved from current `e.Start`) |

### Edge cases (completeness)
| | Scenario | Behavior |
|---|---|---|
| **E1** | bootstrap fails mid-provision | `infra-lock.json` stays; focus stays infra; iterate/close; work preserved (suspended) |
| **E2** | two agents both want infra | X claims; Y refused (operator X **alive**); X closes → Y can claim |
| **E3** | compaction during develop+bootstrap | `status` → "bootstrap step 2/3 (primary) · develop ⏸ suspended" — exact reconstruction |
| **E4** | operator PID dies mid-bootstrap | next `start bootstrap` sees operator **dead** → CAS takeover + resume |
| **E5** | bootstrap added a new runtime | deploying it adds it to the footprint; derived auto-close accounts for it |
| **E6** | PID A launches; A dies; PID B (has its own develop) calls `status` | B's develop stays **primary**; the launch shows as an **overlay**; B can explicitly resume it — not stranded, not hiding develop |
| **E7** | non-operator deploys a service under active bootstrap | warn-and-allow; only a *second infra-op* is hard-refused |
| **E8** | OS reuses a dead operator's PID | start-time mismatch ⇒ **dead** ⇒ reclaimable; cross-user `EPERM` ⇒ **unknown** ⇒ refuse-with-diagnostic; never a false-live wedge or a false-dead steal |
| **E9** | upgrade with in-flight pre-upgrade sessions | persistent state survives; ephemeral sessions cleaned + restarted (§10) |
| **E10** | old + new binary share one state dir mid-rollout | §10 — out of scope by owner decision (no silent auto-update); reopen-on-update |

---

## 8. Validation already performed (so you go beyond it)

| Pass | Method | Result |
|---|---|---|
| **3 audits** (`plans/workflow-family-audit-2026-05-28.md`, `plans/zcp-wholecodebase-audit-2026-05-29.md`, `plans/zcp-workflow-deep-audit-2026-05-29.md`) | drift-hunt + bug-hunt + logic/design lens; default-refute verification | converged on the root-cause (`plans/zcp-architecture-rootcause-audit-2026-05-29.md`): 7 roots; this design is the concrete content of roots R1 (state ownership) + R7 (contract reconciliation) |
| **Break-test** (6-angle, 31-agent) | adversarial: concurrency interleavings, transition completeness, focus/nesting, lock/reclaim/PID-reuse, migration, audit-coverage; each break re-verified default-refute against code | 24 candidate breaks → **12 confirmed/partial, 12 refuted**. Confirmed → the `[BT-x]` amendments now in the design (suspend-while-backgrounded, PID+startTime identity, lock primitive, CAS reclaim, N>1 migration). **Refuted** (do not re-raise): TOCTOU on the flock claim, flock-timeout-as-no-lock, two-process double-reclaim race (the design is already two-slot), several others. |
| **Codex review #1** | independent review of the root-cause audit against code | confirmed the root decomposition; sharpened R1 (state+lifecycle contract, not just a mutex); added R7 (contract drift); corrected the HTTP-readiness finding (two legitimate predicates, not one) |
| **Codex review #2** | independent review of the design (v2) against code | found a **real internal contradiction** (launch as per-PID main vs project-scoped) + 5 holes → the `[CX-x]` fixes: launch→overlay, tri-state liveness (not failure⇒dead), `ResolveLifecycle`-as-overlay (not envelope replacement), dynamic-footprint↔recorder conflict, agent `(pid,startTime)` keying, mixed-fleet migration. All folded in. |

**Survived both adversarial passes (do not re-litigate the concept, only its details):** the focus
rule + single `ResolveLifecycle` (SPINE-1 fix), the singleton-file-as-lock (SPINE-4 fix, with the
read-presence-then-create primitive), `(pid,startTime)` operator identity, develop⇄bootstrap nesting
as two-slot suspend/resume, dynamic footprint, launch storage staying project-scoped.

---

## 9. Decisions (owner — all resolved)

- **Taxonomy** — focus{work, infra-op} + storage{per-PID work, project infra, project launch-overlay};
  cross-process+cross-tool **flock** (a Go mutex is insufficient — cross-process is real); warn-and-allow
  cross-agent deploy under active bootstrap; dynamic footprint; lazy reclaim = the recovery flow.
- **Auto-close** — **KEEP forced auto-close** (invisible — no explicit close step), implemented as a
  **derived phase** (§5). Rejected suggest-close.
- **Migration** — **clean, no compatibility bridge** (§10): no silent auto-update ⇒ updating = user
  reopens sessions ⇒ no long-lived mixed fleet. Persistent state survives; ephemeral session state is
  restarted fresh.
- **`CLAUDE.md:404` exception** — document the narrow exception: `state.Update`'s read-modify-write
  transaction must hold its per-file lock across read+write (releasing between re-introduces the
  lost-update it exists to prevent); safe because it is bounded LOCAL-file I/O, not network.
- **Deferred to another owner:** full integration of `recipe-authoring` (this design only preserves the
  bootstrap↔recipe mutual exclusion, I11).

---

## 10. Migration & backward-compat — clean, no mixed-fleet bridge

**No silent auto-update.** Updating ZCP is user-initiated; on update the user reopens their sessions
(fresh processes, all on the new binary) — or it's a clean install. So there is **no long-lived
old+new fleet sharing one `.zcp/state`**, and the design does NOT build a compatibility bridge.
- **Persistent state survives:** `services/*.json` (ServiceMeta) self-heals via normalize-on-read;
  `launch-production/*.json` (an in-flight prod launch outlives the process) is left as-is,
  project-scoped — the new binary resumes it.
- **In-flight SESSION state is NOT preserved across an update** — it's ephemeral (the user reopens the
  task anyway). On first run of the new binary: clean up stale registry/session/work files, start the
  new model fresh. (This dissolves the N>1-bootstrap-collapse / launch-strand / mixed-fleet
  complexity — those only mattered if in-flight sessions had to survive a concurrent rollout, which
  they don't.)
- **`version` sentinel** gates the one-time, idempotent ServiceMeta normalization.
- **Brief skew window** (old tab still open + new tab opened post-update): the new binary's infra-lock
  isn't honored by the lingering old tab — acceptable; remediation is "reopen after updating",
  optionally a one-line version-skew warning. No bridge.
- **MCP surface:** `status`/deploy response shapes change (launch overlay, derived close) — move
  goldens + docs together; keep old action strings/fields accepted.

---

## 11. Verification checklist for the independent reviewer

**A. Confirm the problem is real** — spot-check the §1.3 evidence table against the code. Especially:
the SPINE-1 two-precedence split (`workflow.go:441` vs `compute_envelope.go:398`); the unlocked
`WriteServiceMeta` + concurrent SDK dispatch (XCUT-1); the phantom exclusivity check (`session.go:181`
comment vs `:203-227` body); `isProcessAlive` EPERM-as-alive; the spec self-contradiction
(`spec-work-session.md` §5.3 vs §6.2).

**B. Stress the design's load-bearing claims** — the items most likely to be wrong:
1. **The two-axis model** (focus vs storage) — is launch-as-overlay genuinely coherent, or does it
   leak the way the v2 main-slot version did? Check the §2 launch rules + E6 against `launch_state.go`.
2. **The lock primitive** — is "read-presence-then-create inside one held flock" actually race-free
   given the repo's flock (`registry.go:128`, `registry_unix.go:25-37`, ~5s timeout)? What happens on
   flock timeout (does it degrade to no-lock)?
3. **Tri-state liveness** — is the Darwin/Linux-container start-time read feasible, and does the
   `unknown ⇒ refuse` branch ever wedge a legitimately-dead operator (cross-user)?
4. **Derived auto-close** — does computing `develop-closed-auto` (vs persisting it) break any existing
   consumer? Check the blast radius: `RecordDeploy/VerifyAttempt`, `compute_envelope.go:387`,
   `envelope.go:70` (phase enum), `build_plan.go:31`, `render.go:150`, `scenarios_test.go:320`.
5. **`ResolveLifecycle` as overlay** — does the `Lifecycle` struct + a phase mapping actually preserve
   today's full status contract (`envelope.go:19-36`: environment, idle scenario, snapshots, attempts,
   bootstrap/recipe summaries)?

**C. Implementability** — flag any mechanism that fights the real code:
- `state.Update[T]` must become the ONLY write path (direct `WriteServiceMeta` RMW is everywhere) —
  feasible to enforce with a lint?
- the `CLAUDE.md:404` "no mutex during I/O" exception — acceptable, or is there a better same-process
  serialization that doesn't need it?
- `ResolveLifecycle` replacing both the dispatcher's `e.sessionID` read and `derivePhase` is a real
  refactor (session-id-addressed loaders) — is the budget realistic?

**D. Find what the two prior passes missed** — they covered concurrency interleavings, transitions,
focus/nesting, lock/reclaim/PID-reuse, migration, audit-coverage, self-consistency, and
implementability. Novel angles welcome: the dynamic-footprint semantics under failed/partial deploys;
the launch-overlay rendering under multiple concurrent launches; interaction with the `adopt` flow
(not modeled here); whether `develop`-only main slot is too narrow (does anything else deserve a focus
slot?).

---

## Appendix — source documents (full detail)
- `plans/zcp-architecture-rootcause-audit-2026-05-29.md` — the 7-root architectural root-cause analysis (Codex-reviewed).
- `plans/zcp-workflow-deep-audit-2026-05-29.md` — finding-level detail (SPINE-*, XCUT-1, DEV-1, launch findings, etc.), default-refute verified.
- `plans/workflow-family-audit-2026-05-28.md`, `plans/zcp-wholecodebase-audit-2026-05-29.md` — the two prior audits.
- `plans/zcp-session-lifecycle-architecture-2026-05-29.md` — the v3 working design (this packet consolidates + supersedes it).
- `docs/spec-work-session.md`, `docs/spec-workflows.md` — the specs to be reconciled (R7).
