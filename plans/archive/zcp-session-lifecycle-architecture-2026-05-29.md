# ZCP Session & Lifecycle Architecture — Design v3 (2026-05-29)

**Why this doc.** The root-cause audit (`plans/zcp-architecture-rootcause-audit-2026-05-29.md`) found
ZCP's worst defects (SPINE-1 status-shows-wrong-thing, SPINE-4 concurrent-bootstrap corruption, the
§5.3-vs-§6.2 spec self-contradiction) all trace to one thing: **no coherent model for sessions and
lifecycle** — two session kinds collapsed into one scalar `Phase` resolved in two places with
opposite precedence. This is the greenfield model that should exist, built to two owner requirements:

1. **develop ⇄ bootstrap nesting is first-class** — an agent in develop can step into a bootstrap
   ("add redis" mid-task) and return to its develop work afterward, by design.
2. **Many parallel sessions (cross-tab AND cross-tool — Claude tabs, Codex, Antigravity on one
   project), but bootstrap is a singleton** — ≤1 active bootstrap per project until it closes.

Bar: *naprosto předvídatelná a excelentně navržená* — every state reachable by one path, every
transition explicit, recovery deterministic, invariants enforced structurally.

> **v3 provenance.** v1 was attacked by a 6-angle break-test (→ `[BT-x]` fixes). v2 was then fully
> reviewed by Codex against the code, which found a real internal contradiction + 5 holes (→ `[CX-x]`
> fixes). The CORE survived both: focus-vs-storage decomposition, single resolver, singleton-lock,
> develop⇄bootstrap nesting. v3's headline change: **launch is a project OVERLAY, not a per-PID focus
> slot** (the v2 contradiction). Reviews: break-test `w3uyumlkt`, Codex `bgucie437`.

---

## 1. Core model — two axes: FOCUS (2 slots) and STORAGE (3 scopes)

The key separation: **"what an agent is doing right now" (focus) is independent of "where state lives
and who recovers it" (storage).** Conflating them was the v2 bug.

### Axis A — FOCUS (per PID): what is *primary* for this agent now

Exactly two focusable slots:

| Slot | Kind | Cardinality | Nests? |
|---|---|---|---|
| **Work** | `develop` | 1 per PID | — |
| **Infra-op** | `bootstrap` | ≤1 per project (the lock) | **yes — nests over work** |

**The focus rule** (resolves "what is primary"; kills SPINE-1):
```
focus(pid) = operatesInfraOp(pid) ? infra-op   // you're driving the bootstrap → foreground
           : hasWork(pid)         ? work        // your develop task (open or suspended)
           : idle
```
> §5.3 ("bootstrap PRIMARY, work backgrounded") and §6.2 ("work-first") were never contradictory
> *rules* — two *focus states* of one machine. Operating the bootstrap ⇒ `focus=infra-op`; it closes
> ⇒ focus falls back to the still-alive work. One rule, one resolver.

### Axis B — STORAGE / recovery scope

| Kind | Storage | Recoverable by | Reaped on PID death? |
|---|---|---|---|
| `develop` (work) | per-PID `agents/{pid}.json` | the owning PID | yes (stale-PID cleanup) |
| `bootstrap` (infra-op) | project `sessions/infra-lock.json` | any live PID (operator died) | **no — survives, reclaimable** |
| `launch` **(overlay)** | project `launch-production/{launchID}.json` | **any live PID on the project** | **no — survives** `[CX-1,BT-F1]` |

### Launch is a project OVERLAY, not a focus slot `[CX-1]`
v2's mistake: calling `launch` a per-PID "main kind" while storing it project-wide + recovering it by
any PID — internally contradictory, and the dispatcher already preempts develop status with launch
recovery (`workflow.go:453`, hiding develop). Corrected: **launch is a project-scoped overlay,
orthogonal to focus.** Its concurrency property is "work-like" (per-agent-initiated, parallel, no
source lock — the owner's "je to proste work"), but it does NOT occupy the focus slot:
- **Generic `status`**: the PID's own `focus` (develop/bootstrap) is primary; an active launch is
  rendered as an **overlay note** ("⟳ production launch in progress on this project: …"), visible to
  every PID. It no longer hides develop.
- **Explicit launch calls** (`start`/continue launch): the handler returns a launch-focused response.
- **No main:** if the PID has no work and a launch is active, the launch overlay becomes the natural
  primary (you're here to launch).
This keeps launch's project-scoped recovery (so a fresh PID after the originator dies still resumes
the in-flight launch — the burned-launchKey path) while removing it from the focus contradiction.

### Recipe is out of scope for this model `[CX, BT-S1]`
`recipe-authoring` is Aleš's separate `zerops_recipe` tool (today `zerops_workflow` rejects recipe and
points there — `workflow.go:549`). It currently shares the single engine session slot with bootstrap
(`engine_recipe.go` → `e.Start`, which refuses a second active session). For THIS design: recipe is
**not** a focus slot here; the only contract is **bootstrap and recipe-authoring are mutually
exclusive** (one infra/authoring session per project). Full integration of recipe into the two-axis
model is deferred to Aleš; this design must not break the mutual exclusion.

**Operator** = the PID holding the infra-op lock, identified by **`(operatorPID, operatorStartTime)`**
— never a bare PID (§2 `[BT-F2,CX-2]`).

---

## 2. Persisted state layout

```
.zcp/state/
  version                          ← schema version sentinel (gates one-way migration) [BT-M2] — see §10 caveat
  services/{hostname}.json         ← ServiceMeta (project evidence; storage unchanged)
  sessions/infra-lock.json         ← THE active infra-op (project-exclusive). bootstrap only.
                                       Presence == the lock. { kind:bootstrap, operatorPID,
                                       operatorStartTime, route, step, startedAt }
  agents/{pid}-{startTime}.json    ← per-PID work (develop) — keyed/validated by (pid,startTime) [CX-5]
                                       { work:{ state:open|suspended|closed, footprint[], intent,
                                                deploys{}, verifies{}, closeSuggestion } }
  launch-production/{launchID}.json ← launch OVERLAY — project-scoped, launchID=hash(srcProj::tgtProj),
                                       any-PID-recoverable. NOT per-PID. [CX-1,BT-F1]
  registry                         ← flock-guarded: live (pid,startTime) + liveness for reclaim
```

- **Project-scoped** (shared across PIDs; every write flock-guarded): `services/`,
  `sessions/infra-lock.json`, `launch-production/`, `registry`, `version`.
- **Per-PID**: `agents/{pid}-{startTime}.json` — keyed by (pid,startTime) so a recycled PID can't
  inherit a dead agent's work; stale-cleanup validates start-time too `[CX-5]`.

**Operator/agent liveness is TRI-STATE `[CX-2]`** (not the bare `syscall.Kill(pid,0)` which reads
`EPERM` as alive — `registry_unix.go:45`). Stamp `(pid, startTime)` (Darwin: `unix.SysctlKinfoProc`
→ `ExternProc.P_starttime`; Linux container: `/proc/<pid>/stat` field 22). Classify:
- **alive** = pid exists AND start-time matches → operator valid; refuse takeover.
- **dead** = pid gone, OR start-time mismatches (PID reused) → reclaimable.
- **unknown** = can't read start-time (e.g. `EPERM`, cross-user) → **REFUSE takeover** with a
  diagnostic (conservative — never steal from a possibly-live operator, never wedge silently).

**The lock primitive `[BT-M1]`**: claim `infra-lock.json` by **read-presence-then-create inside one
held project flock** (absence-test + create share one critical section). NOT `os.Rename`-as-create
(`session.go:168` overwrites → last-writer-wins → both think they hold it). Pin: two concurrent
processes `start bootstrap` → exactly one operator, exactly one non-refused start.

---

## 3. `ResolveLifecycle` — a lifecycle OVERLAY, not a replacement for `ComputeEnvelope` `[CX-3]`

v2 over-claimed: a `Lifecycle` struct cannot replace the envelope (which also carries environment,
idle scenario, project, service snapshots, attempts, bootstrap/recipe summaries — `envelope.go:19`).
Corrected: `ResolveLifecycle` resolves ONLY the lifecycle/focus/phase layer; `ComputeEnvelope` still
computes services/snapshots and **embeds** the lifecycle result. Both the dispatcher status path and
the envelope consume the SAME `ResolveLifecycle` (killing the two-precedence SPINE-1 split — today
dispatcher reads in-memory `e.sessionID` at `workflow.go:441`, envelope reads disk at `derivePhase`).

```go
type WorkState int   // Open | Suspended | Closed
type Lifecycle struct {
    Focus       FocusRef       // Infra | Work | Idle — the actionable primary (§1 rule)
    Work        *WorkSummary   // this PID's develop (state: open|suspended|closed)
    ProjectInfra *InfraSummary // active bootstrap? operator (pid,startTime) + tri-state liveness
    LaunchOverlay *LaunchSummary // project launch in flight (any PID) — rendered as overlay, not focus
    Phase       Phase          // deterministic mapping from the above, for atom filtering + BuildPlan
}
func ResolveLifecycle(stateDir string, pid int) Lifecycle  // consumed by dispatcher AND ComputeEnvelope
```
- A `Closed` work resolves to idle-with-resume-offer — never rendered as a phantom foreground `[BT-S2]`.
- `Phase` is a pure function of the lifecycle, preserving the existing atom-axis/BuildPlan contract.
- Implementation note: bootstrap/recipe status today go through `engine.BootstrapStatus()`/`e.sessionID`
  (`workflow_bootstrap.go:140`); these need session-id-addressed loaders — a real refactor, not a
  drop-in `[CX-E]`.

---

## 4. Scope & close — dynamic footprint + forced auto-close, as a DERIVED phase

**Dynamic footprint** (owner decision): a develop session's scope = the runtime services it actually
deploys. Deploying a runtime **adds it to the footprint**; no upfront declaration. This requires
changing the recorder: today `RecordDeployAttempt` REJECTS out-of-scope hostnames
(`ErrHostnameOutOfScope`, `work_session.go:201`) — v3 instead **adds the deployed runtime target to
`footprint`** `[CX-6]` (managed deps still excluded). Kills DEV-1 (nothing is out-of-scope) and the
old "new-service-into-scope" question.

**Forced auto-close stays — workflows must be as invisible as possible** (owner decision: don't make
the agent do an explicit "close" step the system can infer). When every footprint service is green,
the task is done — no confirmation needed.

**…implemented as a DERIVED phase, not a persisted mutation.** This is the clean realization of
"forced + invisible" that *also* kills the audit's auto-close bug class (gate-desync, lazy-fire from
every surface, status-doesn't-fire-gate). Auto-close is **computed** by `ResolveLifecycle` from state,
never stamped as an event:
```
phase = work.ClosedAt set                         → develop-closed (explicit close)
      : footprint all-green && !operatesInfraOp    → develop-closed-auto   (DERIVED — invisible)
      : work present                               → develop-active
      : else                                       → idle
```
- **No `closeSuggestion`, no `status:"close-suggested"`, no fire/retract event, no per-surface
  `MaybeFireAutoClose`** — the phase is a pure function of the footprint, identical everywhere
  (status, deploy response, envelope), so the gate can't desync. The blast radius *shrinks* vs both
  the current event-based forced close AND the v2 suggest-close: delete `MaybeFireAutoClose`, derive
  instead. (Compaction-safe: pure over state.)
- **Invisible re-engagement:** a derived close auto-reverts the moment a new deploy makes the
  footprint non-green again, and a deploy after a derived close just continues — no explicit reopen.
- **`[BT-S2]` excursion safety:** while `operatesInfraOp(pid)` the work is **suspended** — the derived
  `develop-closed-auto` is NOT computed for it (the `!operatesInfraOp` conjunct above). On
  `close bootstrap` it's re-derived once. The v1/v2 "excursion resumes a dead session" is impossible:
  nothing is persisted-closed behind the excursion; the phase is simply recomputed on return.
- **Explicit close** (agent/user calls `close`) still persists `ClosedAt` — that's a deliberate "I'm
  done," distinct from the derived auto-complete.

*(Internal refactor — derive instead of stamp. No published-behavior change: the agent still
experiences "task auto-completes when green," just computed correctly.)*

---

## 5. Transitions — the complete state machine

| Verb | Precondition | Effect |
|---|---|---|
| `start develop` | no work for (pid,startTime) | create work (open); focus → work |
| `start develop` (force) | work exists, different intent | force-discard prior per existing rule |
| **`start bootstrap`** | `infra-lock.json` absent | claim via flock-critical-section create `[BT-M1]`; operator=(pid,startTime); focus → infra; work → **suspended** |
| `start bootstrap` | present, operator **alive** ≠ me | **REFUSE** — "bootstrap active (operator PID X)" |
| `start bootstrap` | present, operator **dead** (gone / start-time mismatch) | **CAS takeover** under flock (no discarded errors — fixes SPINE-3) `[BT-M3]`; focus → infra |
| `start bootstrap` | present, operator **unknown** (can't read start-time) | **REFUSE + diagnostic** — never steal from a maybe-live operator `[CX-2]` |
| `start bootstrap` | present, operator = me | idempotent |
| `complete`/`skip` step | operator = me | advance `step` |
| **`close bootstrap`** | operator = me | release `infra-lock.json`; work `suspended → open`; phase **re-derived** `[BT-S2]` (if footprint all-green → develop-closed-auto, invisibly); focus → work or idle. Surface "redis ready · web,api auto-complete". |
| `close develop` | work present, NOT operating infra | mark work closed; focus → idle |
| `close develop` | operating infra | **REFUSE** — "close the bootstrap excursion first" (top-down) |
| `reset` | any | mark this PID's work closed; if operator, release `infra-lock.json`; focus → idle (fixes SPINE-2) |
| `start launch` / continue | — | overlay op (does NOT touch the focus slot); launch-focused response; state in `launch-production/{launchID}.json` |
| `close`/terminal launch | — | terminal-state the launch overlay (project-scoped); focus unchanged |
| `status` | any | **read-only** `ResolveLifecycle` → render `focus` primary + launch overlay + project-infra awareness. Reclaim of a dead operator happens on the next **`start bootstrap`**, NOT here `[CX-B]` |

With launch demoted to an overlay, the v2 "`start K2` over an occupied main slot" problem largely
dissolves: the only focus-main is `develop`, and `start launch`/`start bootstrap` don't compete for
it. `status` is purely read-only; the only state-mutating recovery verb is `start bootstrap` (CAS
takeover).

**Required flow:**
```
start develop                  → focus=work; footprint grows as you deploy
start bootstrap "add redis"    → claim infra-lock; focus=infra; work SUSPENDED (close-suggest paused)
close bootstrap                → release; work OPEN; re-evaluate → "redis ready · web,api green — close?"
```

---

## 6. Invariants

| # | Invariant | Enforcement |
|---|---|---|
| **I1** | ≤1 work session per PID | single work record in `agents/{pid}-{startTime}.json` |
| **I2** `[BT-M1]` | ≤1 active infra-op per project | `infra-lock.json` via read-presence-then-create inside one flock; `os.Rename`-as-create forbidden |
| **I3** | a PID may operate infra while holding work (nesting) | `operatesInfraOp` + focus rule; work → suspended (never closed) on infra-enter |
| **I4** | "what is primary" has one answer | one `ResolveLifecycle`, consumed by dispatcher AND envelope (kills SPINE-1) |
| **I5** `[BT-F2,M3,CX-2]` | infra-op survives operator death; reclaimable, never wedged, never stolen-from-live | `(pid,startTime)` identity + tri-state liveness (alive/dead/unknown); unknown → refuse; reclaim = CAS under flock, no discarded errors |
| **I6** | concurrent writes never lose updates | all RMW behind `state.Update[T]` — **every** site (close-mode, git-push-setup, build-integration, first-deploy-stamp, adopt). NOTE: this holds a lock across I/O → a sanctioned exception to CLAUDE.md:404; document it `[CX-E]` |
| **I7** `[BT-S2]` | backgrounded work is suspended; the excursion returns to a LIVE session | suspend on infra-enter; derived `develop-closed-auto` NOT computed while suspended; re-derived once on close bootstrap; nothing persisted-closed behind the excursion |
| **I11** | auto-close is derived, never an event (no gate-desync) | `ResolveLifecycle` computes `develop-closed-auto` purely from footprint-all-green; `MaybeFireAutoClose` deleted; explicit `close` still persists `ClosedAt` |
| **I8** `[CX-1,BT-F1]` | launch state is project-scoped, any-PID-recoverable, never PID-reaped, rendered as overlay | `launch-production/{launchID}.json`; read project-keyed; excluded from stale-PID cleanup; not a focus slot |
| **I9** `[BT-M2,CX-4]` | upgrades migrate state once, losslessly — OR are explicitly unsafe in a mixed fleet | `version` sentinel + N>1-bootstrap-aware migration; mixed-fleet caveat in §10 |
| **I10** `[CX,BT-S1]` | bootstrap and recipe-authoring are mutually exclusive | one infra/authoring engine session per project (preserved from current `e.Start`) |

---

## 7. Edge cases

| | Scenario | Behavior |
|---|---|---|
| **E1** | bootstrap fails mid-provision | `infra-lock.json` stays; focus stays infra; iterate/close; work preserved (suspended) |
| **E2** | two agents both want infra | X claims; Y refused (operator X **alive**); X closes → Y can claim |
| **E3** | compaction during develop+bootstrap | `status` → "bootstrap step 2/3 (primary) · develop ⏸ suspended" — exact reconstruction |
| **E4** `[BT-F2,CX-2]` | operator PID dies mid-bootstrap | next `start bootstrap` sees operator **dead** (start-time mismatch/gone) → CAS takeover + resume |
| **E5** | bootstrap added redis | no fixed scope to "add to" — deploying the new runtime adds it to footprint; close-suggestion accounts for it |
| **E6** `[CX-1]` | PID A launches; A dies; PID B (has its own develop) calls `status` | B's develop stays **primary**; the in-flight launch shows as an **overlay**; B can explicitly resume the launch (project-scoped) — not stranded, not hiding develop |
| **E7** | non-operator deploys a service under active bootstrap | warn-and-allow; only a *second infra-op* is hard-refused |
| **E8** `[CX-2]` | OS reuses a dead operator's PID | start-time mismatch ⇒ **dead** ⇒ reclaimable; cross-user `EPERM` ⇒ **unknown** ⇒ refuse-with-diagnostic; never a false-live wedge or a false-dead steal |
| **E9** `[BT-M2]` | upgrade with 2 live pre-upgrade bootstraps | migrate most-recent into `infra-lock.json`; other left refusal-visible; neither's services stranded |
| **E10** `[CX-4]` | old + new binary share one state dir mid-rollout | §10 — the hard case; sentinel alone is insufficient |

---

## 8. What this fixes from the audits

SPINE-1 (I4) · SPINE-2 (reset) · SPINE-3 (I5 CAS) · SPINE-4/TOPO-2 (I2) · XCUT-1 (I6) · DEV-1 (§4
dynamic footprint) · WF-5 (launch overlay on shared resolver, project-scoped recovery) · Theme C
(two-uncoordinated-lifecycles → two axes + one resolver).

---

## 9. Build sequence (audit R7 + R1)

1. **R7 — reconcile the contract**: rewrite spec §5.3/§6.2/§7.4 to the focus rule + two-axis model;
   strike §6.2 work-first; name recipe's real entry point + the bootstrap↔recipe exclusion.
2. **R1 store** — `internal/workflow/state`: `state.Update[T]` (flock + per-file in-process lock,
   with the documented CLAUDE.md:404 exception), `(pid,startTime)` identity + tri-state liveness,
   flock-critical-section infra-lock claim, `version` + migration. Behavior-preserving first.
3. **`ResolveLifecycle`** — overlay + phase mapping; replace BOTH `derivePhase` and the dispatcher's
   `e.sessionID` read; session-id-addressed bootstrap/recipe loaders.
4. **Transitions** — wire §5; delete the per-hostname bootstrap lock.
5. **Scope/close** — dynamic footprint (recorder adds runtimes) + suggest-close migration (§4 blast
   radius); retire forced auto-close, keep legacy read support.

---

## 10. Migration & backward-compat — clean, no mixed-fleet bridge `[CX-4 — owner decision]`

**No silent auto-update.** Updating ZCP is user-initiated; on update the user reopens their sessions
(fresh processes, all on the new binary) — or it's a clean install. So there is **no long-lived
old+new fleet sharing one `.zcp/state`**, and the design does NOT build a compatibility bridge
(owner: "rovnou ať je to čisté"). Concretely:

- **Persistent state survives** (it's real, not session bookkeeping): `services/{hostname}.json`
  (ServiceMeta) self-heals via normalize-on-read; `launch-production/{launchID}.json` (an in-flight
  prod launch outlives the process) is left as-is, project-scoped — the new binary resumes it.
- **In-flight SESSION state is NOT preserved across an update** — it's ephemeral (tied to a task the
  user is reopening anyway). On first run of the new binary: clean up stale registry/session/work
  files (old bootstrap/develop sessions are abandoned-on-reopen, by design), start the new session
  model fresh. This dissolves the N>1-bootstrap-collapse + launch-strand + mixed-fleet complexity the
  break-test/Codex flagged — those only mattered if in-flight sessions had to survive a concurrent
  rollout, which they don't.
- **`version` sentinel** still gates the one-time, one-way ServiceMeta normalization so it's
  idempotent on repeated new-binary runs.
- **Brief skew window** (old tab still open + new tab opened post-update, user didn't reopen): the new
  binary's infra-lock isn't honored by the lingering old tab. Acceptable per the owner — the
  remediation is "reopen your sessions after updating"; optionally emit a one-line version-skew warning
  if an old-format live session is detected. No bridge.
- **MCP surface**: `status`/deploy response shapes change (launch overlay, derived close) — move
  goldens + docs together; keep old action strings/fields accepted.

---

## 11. What survived both reviews (do not re-litigate)
The focus rule + single `ResolveLifecycle` (SPINE-1 fix), the singleton-file-as-lock concept
(SPINE-4 fix, with the M1 primitive), `(pid,startTime)` operator identity, develop⇄bootstrap nesting
as two-slot suspend/resume, dynamic footprint, and the storage fix for launch (project-scoped) all
held. v3's changes *complete* the model (launch→overlay, tri-state liveness, ResolveLifecycle-as-
overlay, suggest-close-as-migration) rather than replacing it.

---

## 12. Decisions — ALL RESOLVED (owner, 2026-05-29)

- **Taxonomy** — focus{work, infra-op} + storage{per-PID work, project infra, project launch-overlay};
  cross-process+cross-tool flock; warn-and-allow cross-agent deploy; dynamic footprint; lazy reclaim =
  the recovery flow (via `start bootstrap`, tri-state liveness).
- **Auto-close** — **KEEP forced auto-close** (workflows must be invisible — no explicit close step the
  system can infer). Implemented as a **derived phase** (§4), not a persisted event: invisible AND it
  kills the audit's auto-close bug class. (Rejected suggest-close.)
- **Migration** — **clean, no compatibility bridge** (§10). No silent auto-update ⇒ updating = user
  reopens sessions ⇒ no long-lived mixed fleet. Persistent state (ServiceMeta, in-flight launch)
  survives; ephemeral session state is cleaned + restarted fresh.
- **CLAUDE.md:404 exception** — **document the narrow exception**: `state.Update`'s RMW transaction
  must hold its per-file lock across read+write (releasing between would re-introduce the lost-update
  it exists to prevent); safe because it's bounded LOCAL-file I/O, not network. Add the exception note
  to CLAUDE.md when `state.Update` lands.

**Deferred to Aleš:** full integration of `recipe-authoring` into the two-axis model (this design only
preserves the bootstrap↔recipe mutual exclusion, I10).

**→ Spec-ready.** Next: R7 (reconcile spec §5.3/§6.2/§7.4 to this model) → R1 (`internal/workflow/state`
+ `ResolveLifecycle` + transitions), per §9.

---

*v3 is break-test- + Codex-hardened, all §12 decisions resolved → SPEC-READY. `[BT-x]` = break-test
fix, `[CX-x]` = Codex-review fix.*
