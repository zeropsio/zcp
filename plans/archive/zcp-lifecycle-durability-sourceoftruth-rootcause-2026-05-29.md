# ZCP lifecycle root-cause audit — durability legibility + source-of-truth seam

Date: 2026-05-29
Inputs: agent eval transcripts `.zcp/manual/e2.txt` (Bun weather dashboard, dev-mode),
`e3.txt` (Laravel blog, recipe, dev+stage, prod deferred), `e4.txt` (redesign homepage,
"only dev, leave staging").
Method: each transcript-agent finding treated as a HYPOTHESIS, verified against real
code by an 8-agent adversarial workflow + independent reading of the connective layer
(`compute_envelope.go`, `work_session.go`, `service_meta.go`, `render.go`,
`topology/runtime_class.go`, `ops/verify_checks.go`, `ops/dev_server*.go`,
`tools/workflow_develop.go`, `tools/launch_source_control_gate.go`). The transcript
agents' own conclusions are NOT trusted — two are misdiagnoses (F6, F8), three are
correct-symptom/wrong-layer (F1, F2, F5).

---

## 1. Findings, reclassified against the code

| # | Transcript / agent claim | Verdict | Real locus |
|---|---|---|---|
| **F1** | e2: dev-mode auto-closed `auto-complete` + verify `healthy`, then 502 — "success overstates durability" | **REAL-MATERIAL**, agent under-ranked the layer | verify probes the live L7 route + gate has no durability axis (symptom is the close note) |
| **F2** | e2: mode default `dev` for a durable web app; tradeoff not surfaced | **REAL-MATERIAL**, agent mis-located | `bootstrap-mode-prompt.md` *does* render — but on the **wrong axis** (iteration-frequency, not durability). No intent→mode steering exists |
| **F3** | e3: launch-production hard-requires git remote; dev/stage direct-push leaves code nowhere git-addressable | **REAL-FUNDAMENTAL** (most accurate of the 8) | genuine source-of-truth discontinuity. *Agent error:* "launch could self-package via export" — export ALSO requires a pre-existing user remote (`bundle/inputs.go`: RepoURL empty rejected) |
| **F4** | e3: recipe CLAUDE.md hardcodes `appdev`; remap→blogdev points docs at wrong service | **MINOR** | cloned CLAUDE.md is the *external* buildFromGit repo's; rewrite (`recipe_override.go`) is YAML-only by design. *Agent missed:* ZCP's OWN synced `laravel-minimal.md:14,16` carries the same `ssh appdev` hardcoding — recipe-authoring scope. Danger never materialized |
| **F5** | e4: "leave staging alone" inexpressible; `scope=[appdev]` "doesn't change the close gate" | **REAL-MATERIAL** | `scope` IS the gate's sole input (`ws.Services`) — it is **silently PRE-WIDENED**: `validateDevelopScope:361-378` appends the stage half unconditionally. Not "ignored" — widened |
| **F6** | e4: ADOPT_REQUIRED = 4-call heavyweight ceremony for ACTIVE services | **MINOR**, agent mis-framed | actually **6 calls** (under-counted); close auto-skips; provision is NOT a no-op (live recheck + env catalogue). Proposed fix (auto-adopt-on-first-call) is **WRONG** — would force synthesizing pairing/mode that `InferServicePairing` exists to avoid. Real friction = avoidable route-menu round-trip + a redundant `includeEnvs` discover the atom steered into |
| **F7** | e4/e3: develop start dumps ~7K undifferentiated tokens, "full cost every session" | **REAL-MATERIAL**, headline figure wrong | e4 (iterate) rendered ~4.4K/15 atoms; the 7K/exotic belongs to e3's **first-deploy** branch — `deployStates` already branches, so "every session pays full cost" is refuted. Real defect: no managed-type-presence + task-class axes; monolithic `develop-first-deploy-env-vars.md` |
| **F8** | e4: `Next: deploy` contradicts "blade reinterpreted live, no redeploy needed" | **AGENT MISDIAGNOSED — got it backwards** | appdev = `mode=standard` php-nginx → `IsDeferredStart=false`. The SSHFS edit is **visible but NOT in the stored artefact** (`deploy-process.mdx:14-31`: every new container re-downloads the artefact, incl. scale + failure-restart) → reverts on cycle. `Next: deploy` was **correct**; the agent confused VISIBILITY with DURABILITY and would have shipped a phantom change |

---

## 2. Mental model — how the agent EXPERIENCES the wiring

ZCP's live-interaction surface — the **SSHFS mount + the `zerops_dev_server` process** — is
a **TRANSIENT OVERLAY on an artefact-from-storage container model**. What the agent SEES
live (a 200 from the subdomain; a blade edit rendering on the next request) is ephemeral.
Only **(deployed artefact) + (a supervised `run.start` process)** survive a container cycle.

Three faces of durability the agent must reconcile alone, with no single statement of:

| Service shape | Process durable? | Live edit durable? | `IsDeferredStart` |
|---|---|---|---|
| dev-mode dynamic (Bun, e2) | **No** — process is the ephemeral dev_server; dies on cycle | n/a (deployed code persists, process doesn't) | **true** (502 expected until dev_server start) |
| standard / simple dynamic | yes (`run.start` supervised) | **No** — SSHFS edit not in artefact, reverts on cycle | false |
| implicit-web php-nginx (e4) | yes (webserver auto-starts from artefact) | **No** — SSHFS edit not in artefact, reverts on cycle | false |

The connective failure: **ZCP owns this truth precisely** — `topology.IsDeferredStart`
(`runtime_class.go:69-74`), the `run.start is empty` warning (`deploy_validate.go:60-62`),
the artefact model (`deploy-process.mdx:14-31`) — and **three DEPLOY-side surfaces consult
it** (`deploy_subdomain.go:137`, `subdomain.go:156`, `next_actions.go:54`). But the two
**TRUST-bearing surfaces** and the two **DECISION surfaces** never do:

- **verify** (`ops/verify_checks.go`) — `checkHTTPRoot` probes the **public subdomain** and
  passes on any `<400`; `checkServiceRunning` only reads platform `Status==RUNNING/ACTIVE`
  (true by design for any `startWithoutCode` container). Uses its OWN runtime enum
  (`classifyRuntime:49`), divorced from topology. Zero `IsDeferredStart` references.
  → answers "is the L7 route serving THIS INSTANT?", which a transient process or an
  undeployed-but-mounted edit both satisfy.
- **auto-close gate** (`work_session.go:379-396` `EvaluateAutoClose`) — = (every resolved
  target: last deploy `SucceededAt` + last verify `Passed`) AND (every in-scope meta
  `CloseDeployMode ∈ {auto, gitPush}`). **No `run.start`/durability input.**
  `CloseReason = "auto-complete"` literally means "deployed+verified", nothing about
  survival. `MaybeFireAutoClose` re-runs it lazily on every lifecycle response.
- **mode selection** (`bootstrap-mode-prompt.md`) — frames dev/simple on
  iteration-vs-worker, never on "what stays up on its own."
- **Next-hint** (`render.go:128-137`, `build_plan.go:148-152`) — keyed on **this session's**
  recorded deploy attempts (`needsDeploy = !lastSucceeded(ws.Deploys[host])`), not the
  persistent `Deployed` flag. (Correct behavior — it's what held e4 open — but its REASON
  is invisible, so the agent read it as a false positive.)

Two further structural facts complete the picture:

- **`scope` is pre-widened** (`workflow_develop.go:361-378`): any standard dev half drags
  its stage half into `ws.Services`, the gate's sole input. `render.go:94` denominator is
  `len(ws.Services)` post-expansion → "0/2". "Iterate dev only" is structurally
  inexpressible — no exclusion field; `CloseDeployMode` is a 4-value closed enum with no
  skip value.
- **git-push is a separate source-of-truth island**: bootstrap inits
  `GitPushUnconfigured`; **only** `handleGitPushSetup` writes `Configured`. Direct/cross
  deploy never sets it. Launch hard-requires `Configured` + live-matching user remote
  (`launch_source_control_gate.go:165-216`). Export ALSO requires a remote. The dev/stage
  happy path (mutable workspace + direct push + recipe `buildFromGit` = non-user template
  repo) never establishes the git substrate prod demands — discovered reactively at the
  prod wall.

---

## 3. Shared root causes (the few upstream causes beneath the 8)

### RC-A — Durability is never a first-class axis on the success-signal or decision surfaces
**Explains F1, F2, F8** (and feeds F7's missing-axis theme). Layer: workflow + content
(the predicate is already correct in topology).
The system simultaneously KNOWS (deploy side) and DENIES (verify + close + mode-choice)
that a service is non-durable. The single missing concept: **"is the current observable
state == the durable artefact state, and will it survive a container cycle?"** — surfaced
at choose-mode, verify, close, and Next.

### RC-B — Pair-keyed PERSISTENCE invariant illegitimately lifted into a SESSION-COMPLETION invariant
**Explains F5, the appstage Next-hint in F8, part of F6.** Layer: workflow.
The pair-keyed `ServiceMeta` (one meta per dev/stage pair) is a correct *persistence*
shape. `validateDevelopScope` lifts it into a *session-completion* requirement by
auto-widening scope. This conflates "remind the agent to promote" (guidance) with "session
incomplete until stage deployed" (gate). The pinned scenario
`existing-standard-appdev-only-reminders.md` shows the *intended* behavior was reminders,
not blocking — the widening overshot it.

### RC-C — Two incompatible source-of-truth models with only a reactive bridge
**Explains F3, and F4 as its recipe-layer twin.** Layer: workflow (missing bridge) +
recipe/content (contributing).
DEV/STAGE = mutable-workspace (live `/var/www`, direct push, `GitPushUnconfigured`, recipe
`buildFromGit` = non-user template repo). PRODUCTION = immutable-git-build (user-owned
remote, hard-enforced). **`git-push-setup` is the ONLY thing that establishes the
prod-required substrate** — both `launch` and `export` consume a remote, neither creates
one. The happy path never establishes it; the transition is reactive (no develop atom
anticipates it — `develop-close-mode-git-push.md` is itself gated behind
`gitPushStates:[configured]`). F4 is the same class one layer down: recipe-cloned content
(`appdev` CLAUDE.md, `.git/config` template URL, synced `.md` prose) is
**stale-by-construction** relative to the deployed/remapped state.

### RC-D — Ceremony and guidance aren't right-sized to the task
**Explains F6, F7.** Layer: workflow + content.
Adopt is forced through the route-menu front door even when adopt is the only viable route,
plus a redundant `includeEnvs` discover. The atom selector's `AxisVector` (`atom.go:58-127`)
has no **managed-type-presence** axis (so `develop-first-deploy-env-vars.md` ships all of
Postgres/Kafka/ClickHouse/S3/Meili as one block despite `ServiceSnapshot.TypeVersion` being
available and `RuntimeBases` axis existing unused) and no **task-class** axis (Intent flows
to the envelope but `Synthesize` never reads it). The heavy atoms are service-AGNOSTIC, so
the `scope` param prunes almost nothing.

---

## 4. Architecture proposal (root-cause-level, not symptom patches)

### A. Make durability a first-class, surfaced dimension (RC-A) — HIGHEST LEVERAGE
The fix is **legibility, not prohibition** — e2's user, when asked, legitimately chose to
keep dev mode; `*-dev-only*` scenarios treat dev-only+200 as valid terminal success.

1. **One durability predicate, consulted everywhere.** Promote a
   `topology.DurabilityVerdict(mode, class, runStartPresent)` (built on the existing
   `IsDeferredStart`) → `{durable | needs-supervisor-process (dev-mode) | live-edit-not-yet-deployed}`.
2. **verify** carries it: for a deferred-start service, a `service_running`/`http_root`
   pass MUST be annotated "served by the dev-server process — not supervised; 502 after a
   container cycle until `zerops_dev_server start`." verify should also state that
   `http_root` reflects the **live container**, and durable state = last deployed artefact.
3. **auto-close / close note** carries it: a dev-mode service with empty `run.start` may
   still auto-close (user's right) but the close reason must NAME the non-durability instead
   of bare `auto-complete` — e.g. `auto-complete (dev-mode: appdev is not supervised; will
   502 after a container cycle — switch to simple for an always-on service)`.
4. **mode selection** reframed: change `bootstrap-mode-prompt.md`'s axis from
   iteration-frequency to **durability/reachability**, and add **intent→mode steering**
   (single web-facing service the user wants reachable, no iteration signal → recommend
   `simple`, not `dev`; `dev` is the scaffolding phase, not the terminal state).
5. **Next-hint reason** (F8): keep the hint (it's correct) but surface WHY — "your edit is
   live but not in the deployed artefact; deploy to make it survive a container cycle."

### B. Decouple session-completion from the persistence pair (RC-B)
- Stop widening in `validateDevelopScope`: `scope` passes through as the literal session
  unit. The stage half becomes a **Next-hint/guidance reminder** ("stage `appstage` is
  stale relative to dev; promote when ready"), NOT a close-gate blocker.
- Keep the pair-keyed META (persistence) untouched. Only the SESSION's pending set changes.
- Fix the `render.go:94` denominator to read `len(ResolvedDeployTargets)` (the gate's real
  count) so the agent-facing ratio can't diverge from the gate (completeness-gap #3).

### C. Surface the source-of-truth bridge proactively (RC-C)
- When prod intent is captured (bootstrap intent mentions production, or at develop close
  for a recipe-bootstrapped project whose `buildFromGit` is a non-user template repo),
  surface `git-push-setup` as an **early, optional** step — not a reactive gate at the prod
  wall. The fact to teach: "your code currently lives in a template repo + direct-push; for
  production later you'll need your own repo + PAT via `git-push-setup`."
- F4 (recipe-authoring scope, flag to Aleš not act): template hostnames in recipe-authored
  CLAUDE.md / synced `.md` prose, OR have ZCP warn at remap that recipe docs reference the
  original hostname. Add a recipe-lint rule for literal runtime-hostname strings in prose.

### D. Right-size ceremony + guidance (RC-D)
- **F6:** generalize the one-shot adopt path (`InferServicePairing`, `LocalAutoAdopt`
  already prove it) — when discover detects adoptable services and adopt is the only viable
  route, skip the route-menu round-trip and the redundant `includeEnvs` discover. (NOT
  auto-adopt-on-first-call — that forces synthesizing pairing/mode.)
- **F7:** add a **managed-type-presence axis** to `AxisVector` (env-channel atoms show only
  dep types in scope) and a **task-class axis** (derive from deployed-state + edit-vs-infra,
  or read the Intent already on the envelope). Split monolithic
  `develop-first-deploy-env-vars.md` by dep type.

---

## 5. Priority, regression risks, open questions

**Priority:** A (durability legibility — through-line, explains F1/F2/F8) > B (pair vs
completion — clean, contained) > C (source-of-truth bridge — fundamental, fix is sequencing
not mechanism) > D (right-sizing — real, lower severity).

**Regression risks / open questions (workflow-flagged gaps to resolve before coding):**
1. **A must not regress the pinned `*-dev-only*` scenarios** (`bootstrap-dev-only-bun-health`
   etc.) that encode dev-only+200 as clean terminal success. The fix is annotation, not a
   block — verify this holds against the pins before shipping.
2. **Container-cycle frequency** (severity of F1/F8) is asserted, not empirically pinned —
   does an idle dev-mode service actually sleep/cycle, and how often? `deploy-process.mdx`
   confirms re-pull on scale + failure-restart (so F8 holds regardless), and e2's 502
   actually occurred, but the idle-sleep cadence wasn't measured live.
3. **status-recovery path** (`action=status` after compaction) re-renders through
   `ComputeEnvelope` — does it re-apply `validateDevelopScope` widening (re-widening a
   previously-narrowed scope)? Not traced; check before fixing B.
4. **`render.go:94` vs `EvaluateAutoClose` denominator** can already diverge for git-push
   delivery (targets collapse to 1, `ws.Services` stays 2) — a secondary confusion surface
   beyond F5.

**Effort (rough):** A ≈ topology predicate + 3 surface call-sites + 2 atoms (~2 days). B ≈
scope-passthrough + reminder rendering + denominator fix (~1.5 days). C ≈ proactive
git-push surfacing atom + intent detection (~1 day; F4 is Aleš). D ≈ axis additions +
adopt-fast-path + atom split (~2 days). Total ≈ 6–7 days if all four; A+B are the
high-leverage core (~3.5 days).

---

## 6. Round 2 — Codex (gpt-5.5) adversarial opposition, 2026-05-29

Codex independently verified the load-bearing claims against code + `../zerops-docs/` and
**confirmed F8, F3, F1, F6, and the RC-A/RC-B/RC-C diagnoses** (the F8 platform fact is now
double-confirmed: `deploy-process.mdx:12,28` + `php/.../deploy-process.mdx:6` — a new
container downloads the *stored artefact*, so the un-deployed SSHFS edit reverts). It landed
two sharpenings and three missed issues. **Adopt all five.**

### Sharpening 1 — the unifying frame is STATE-CONFLATION, not just durability
The true meta-pattern is that ZCP conflates **three distinct notions of "done"**:
**live-observed** (verify's HTTP 200 / a visible edit) vs **session-complete** (the
auto-close gate) vs **durably-delivered** (in the stored artefact + supervised). "Durability
is invisible" is the e2/e4/F8/F2 slice of this; RC-B (scope) and RC-C (source-of-truth) are
*separate* lifecycle-contract conflations, not the same missing bit. Reframe the analysis as
"ZCP has no single honest definition of done/delivered," with durability as one axis and the
mutable-workspace-vs-git-build split as another.

### Sharpening 2 — RC-A "legibility, not prohibition" is a symptom-patch as written
If e2 still auto-closes `auto-complete` and the user gets a 502 later, annotation alone
leaves the failure in place. The stronger fix (which my RC-A already contained but
under-billed): a **distinct completion contract** — a deferred-start (dev-mode dynamic) web
service is **"live now / transient," NOT "durably delivered."** Combined with intent-aware
mode steering at decision time. This still doesn't *prohibit* dev mode (e2's user explicitly
chose to keep it; `*-dev-only*` scenarios are legitimate) — it just refuses to *call it
delivered*. RC-A's headline changes from "annotate" to "introduce a durably-delivered
completion contract that dev-mode-web does not satisfy."

### Sharpening 3 — RC-B fix must be an explicit per-session scope state, not a "reminder"
A non-blocking reminder reintroduces exactly the stale-stage failure the auto-widen was
built to prevent (`workflow_develop.go:314`). **Better:** an explicit session-scoped state
per service — `required | deferred | out-of-scope` — distinct from the pair-keyed
`ServiceMeta`. Default for a standard pair stays **required** (preserves anti-stale-stage);
"leave staging as it is" marks `appstage` **out-of-scope this session**. This is the clean
version of RC-B and resolves the regression risk I flagged.

### Sharpening 4 — RC-C trigger is EXPLICIT prod intent, not "might go prod"
Surfacing git-push-setup for any project that *might* launch adds friction to the 90% that
never do. Trigger on **explicit prod/export intent** (e3 literally said "chci stage a
produkci" → should have signposted at bootstrap). The reactive hard gate at launch is
*correct*; the defects are (a) no early signposting on explicit intent, (b) **no first-class
"prod-deferred-pending-source-control" state owned by ZCP** — which is also why the e3 agent
wrote prod-deferral into its own `.claude` memory (see Missed #3).

### Missed issues (none of F1–F8 captured these — all genuine)
1. **e4 terminal-state honesty failure (`e4.narr.txt:648`).** The agent declared "done"
   while the work session was still **open with pending required work and no in-session
   deploy**. ZCP needs a **final-response guard**: an open session with pending *required*
   services must not be presentable as complete. This is the agent-side mirror of F1's
   done-honesty gap; it builds directly on RC-B's clean session-completion model.
2. **Contradictory guidance in the same context.** ZCP's develop guidance says
   standard/simple/local → `zerops_deploy` (`e4.narr.txt:61`), while the recipe-shipped
   CLAUDE.md says blade edits are live — both authoritative-looking, and the agent followed
   the wrong one. The `Next:` hint must state **"deploy for durability,"** and recipe prose
   (F4) must not contradict the platform contract. Ties F4 + F7 + F8 together.
3. **e3 project-env restart isolation hazard (`e3.narr.txt:50`).** Setting project-level
   `APP_KEY` for the Laravel blog **restarted the unrelated existing `appdev` (the Bun
   weather dashboard)**. Mechanically expected (project-scoped env), but a real
   **workflow-isolation hazard** in shared/multi-app projects: a bootstrap for app B bounced
   running app A. Worth a warning when a project-env write will restart out-of-scope
   services.

### Completeness gaps — adjudicated
- **Container-cycle frequency:** load-bearing for *severity*, not *correctness* (docs prove
  the artefact model; e2's 502 actually occurred). Leave as a severity caveat.
- **Status-recovery re-widening:** **NOT a problem** — `action=status` renders the existing
  envelope (`tools/workflow.go:1039`); it does not re-run `validateDevelopScope`. Open
  question #3 is closed.
- **`render.go:94` denominator:** real but narrow UI bug — auto-close uses
  `ResolvedDeployTargets` (`work_session.go:463`), render displays raw `ws.Services`
  (`render.go:94`). Fold the one-line fix into RC-B.

### Revised priority (Codex + dependency analysis)
**RC-B → RC-A′ → RC-C → RC-D.** RC-B (explicit `required|deferred|out-of-scope` session
state) is the **foundation**: the strengthened RC-A′ completion contract ("durably
delivered") and Missed #1 (open-session-done guard) both need a clean notion of "which
services are *required* this session" — which is exactly what RC-B provides. So B is no
longer just contained, it's a prerequisite. A-as-originally-written (annotate) was too weak
to ship first; A′ (completion contract + intent-aware mode) is worth shipping right after B.

---

## 7. Round 3 — reconciliation against unpushed `main` commits (2026-06-01)

This analysis (Rounds 1–2) was done **2026-05-29** against then-current code. Since then **21
commits landed on local `main`, unpushed to `origin/main`** (all 2026-06-01). The largest is
a **session-lifecycle rebuild (P0–P7)** that rewrote the auto-close machinery — the exact
code RC-A/RC-B cite. Verdict after re-reading current code: **no commit invalidates any RC;
one mechanism citation in RC-A is now stale; the rebuild actually improves the landing
surface for RC-A and RC-B.**

### What the session-lifecycle rebuild changed (P0–P7)
Per-hostname exclusivity, one `withFileLock` primitive, one disk precedence resolver
(`ResolveLifecycle`), `(pid,startTime)` identity, and — load-bearing for us — **derived
auto-complete: `MaybeFireAutoClose` DELETED**, close state now computed on every read via
`DeriveCloseState` → `EvaluateAutoClose` → `serviceAutoCloseReady`, with `IsOpen` the single
open-predicate (`work_session.go:370,409,417,542`). The rebuild's stated scope (per
`plans/session-lifecycle-rebuild-2026-05-29.md`) is **session identity + locking + derived
close + scope-as-denominator** — it explicitly does NOT touch durability, the 502 surface,
dev-mode supervision, or terminal-state honesty (grep confirms zero mentions).

### Per-RC reconciliation against current code

| RC | Touched by unpushed commits? | Verdict | Action |
|---|---|---|---|
| **RC-A** | auto-close machinery rebuilt (P5); `verify_checks.go` **untouched** | **HOLDS.** `serviceAutoCloseReady` (`work_session.go:542`) is still `(last deploy succeeded) AND (last verify passed)` — **no durability axis**. verify still live-surface-only. | **Re-cite to the derived model AND correct the landing (see ⚠ below).** The durability fix does NOT go "for free" into `DeriveCloseState`. |
| **RC-B** | `workflow_develop.go` touched by P6/P7 (`9137f187`,`70e4d493`); `render.go` untouched; `CloseDeployMode` enum unchanged (still no skip value) | **HOLDS.** Despite P6/P7 edits, `validateDevelopScope:367` auto-widen logic is intact. | The rebuild **cemented "declared scope = the auto-close denominator"** (plan §2) and a single-source close model — exactly the substrate RC-B's `required\|deferred\|out-of-scope` modifies. Fit is now *tighter*. |
| **RC-C** | gate **logic CHANGED** by LAUNCH-3/4 (`6741e2ca`): new `runReadSideSourceControlGate` loops every promoted runtime (`launch_source_control_gate.go:596`), wired before key-mint (`workflow_launch_production.go:152`) | **HOLDS — and the gate got STRONGER.** Launch/export still consume a remote; gate still requires `GitPushConfigured`; now multi-runtime-aware. | `f05115ed`: `GitPushConfigured` now stamped only on a *confirmed* restart (`workflow_git_push_setup.go:441`) → substrate more trustworthy. `9f3e4e83`: service USER envs now travel as `envSecrets` (`bundle/inputs.go:70`) → the "what reaches prod" set widened. `50375605`: inert export `ExportVariant` deleted (stale comments remain at `workflow_export.go:18`). None bridge the seam. |
| **RC-D** | `synthesize.go` **untouched** (F7 holds); adopt route-menu ceremony **untouched** (F6 holds) | **HOLDS.** `adopt_local.go` change is only the `NewServiceMeta` constructor swap, not flow weight. | No change. |

> **⚠ CORRECTION (Codex round 3) — RC-A′ must NOT inject a live durability check into `DeriveCloseState`.**
> Derived close is **intentionally pure over disk** and must stay byte-deterministic via the
> stable `LastActivityAt` (`work_session.go:389`, rebuild plan §71) so the envelope survives
> compaction recovery. A durability predicate that reads **live** dev-server/platform liveness
> would make the *same disk state* render closed→open→closed as the dev server starts/stops or a
> platform read fails — strictly worse than the deleted stamped model. **Correct shape:** stamp a
> **disk-stable per-host `durabilityVerdict`** from *stable* inputs only — `Mode`, `RuntimeClass`,
> and the `run.start`/in-session-deploy snapshot (all knowable without a live read;
> `IsDeferredStart` is a pure `(mode,class)` function) — at deploy/verify time or on RC-B's
> per-session target state. `DeriveCloseState` then *consumes* the persisted verdict. Live
> liveness stays in `verify` and the close-note, never in the derived gate.

### Complementary work already landed (same spirit — narrows or strengthens our fixes)
- **TOPO-1 (`d9dc0895`):** `normalizeDeployDims` heals empty `CloseDeployMode`/`GitPushState`/
  `BuildIntegration` on every snapshot so the git-push-setup + build-integration atoms fire
  for local-stage metas (`compute_envelope.go:209-230`). Same *dimension-legibility* class as
  RC-C / F7 — confirms the team is already fixing "silently-empty dimension → atom never
  fires." RC-A's durability dimension would slot beside these three.
- **WF-1 (`3dd65d4c`):** `DeployIntent.Resolve` honors the canonical setup name
  (`setupOrDefault`) — does NOT touch the self/cross delivery-class or durability model.

### Codex missed-issues status post-rebuild
- **Missed #1 (open-session-done guard):** NOT addressed, but the rebuild's `IsOpen`
  (`work_session.go:409`) is now the clean predicate to build the final-response guard on.
- **Missed #3 (project-env restart isolation):** NOT addressed. The env-lifecycle audit
  (`plans/zcp-env-correctness-*`) documents that project-env propagates *in-place via the
  zembed daemon (~5–10s, no restart)* — yet e3 showed `restartedServices:[appdev]`. ZCP
  surfaces `RestartedServices` (`tools/env.go:170`) but adds no out-of-scope-restart warning.
  **Hand to the env-lifecycle workstream**, not this plan.

### Net
No unpushed commit invalidates any RC. Forced edits from the unpushed code: (1) RC-A re-cites
the derived close model AND the durability fix must persist a disk-stable verdict, NOT read
live state in `DeriveCloseState` (⚠ above); (2) RC-B's "untouched" was wrong — file edited by
P6/P7, logic intact; (3) RC-C's gate logic DID change (LAUNCH-3/4, now multi-runtime + a
trustworthier git-push stamp + a wider env-secrets payload) — all *strengthen* the existing
model, none bridge the dev/stage↔prod seam. Priority unchanged: **RC-B → RC-A′ → RC-C → RC-D**.

---

## 8. FINAL — consolidated recommendation (current-code-accurate, 2026-06-01)

This supersedes Rounds 1–3 for review. It is written against current `main` (21 unpushed
commits incl. the session-lifecycle rebuild) and incorporates both Codex passes.

**The one meta-cause:** ZCP has no single honest definition of *done/delivered*. It conflates
three notions — **live-observed** (verify's HTTP 200 / a visible SSHFS edit) vs
**session-complete** (the derived auto-close gate) vs **durably-delivered** (in the stored
artefact + a supervised process). Plus a parallel **source-of-truth** conflation
(mutable-workspace direct-push for dev/stage vs immutable-git-build for prod). Every transcript
failure is one of these conflations surfacing.

**Verified true against current code** (file:line): verify is live-surface only
(`verify_checks.go:65,149`); the derived close gate `serviceAutoCloseReady` is deploy+verify
only, no durability axis (`work_session.go:542`); `validateDevelopScope` auto-widens a standard
dev half to its stage half (`workflow_develop.go:367`); `CloseDeployMode` has no out-of-scope
value (`topology/types.go`); `git-push-setup` is the only `GitPushConfigured` setter, launch +
export only consume a remote (`launch_source_control_gate.go`, `bundle/inputs.go`,
`bundle/export.go`); `IsDeferredStart` is a pure `(mode,class)` fact already known to the
deploy side but not to verify/close (`runtime_class.go:69`).

### The four fixes, in ship order

**1 — RC-B: explicit per-session service role (FOUNDATION, ship first).**
Add a session-scoped per-service state `required | deferred | out-of-scope`, distinct from the
pair-keyed `ServiceMeta`. Stop the unconditional stage auto-widen in `validateDevelopScope` —
instead default a standard pair's stage half to **required**, but let the agent mark it
**out-of-scope** ("leave staging as it is" → `appstage` out-of-scope this session). The derived
auto-close denominator (already "declared scope" per the rebuild) reads only `required`
services. Fix the `render.go:94` denominator to match `ResolvedDeployTargets`. This is the
prerequisite for #2 and #4 (both need a clean "which services must be done this session" set).
*Why first:* contained, unblocks the e4-class immediately, and is the substrate the other fixes
stand on. *Trade-off:* default-required preserves the anti-stale-stage behavior the widen was
built for; only an explicit signal opts out.

**2 — RC-A′: a durably-delivered completion contract (ship after #1).**
Persist a **disk-stable per-host `durabilityVerdict`** computed from stable inputs only —
`Mode`, `RuntimeClass`, `run.start` presence, and in-session-deploy state — stamped at
deploy/verify time (or on #1's per-session target record). Values: `durable` /
`transient-dev-process` (deferred-start dev-mode dynamic) / `live-edit-not-yet-deployed`.
Then: (a) `verify` annotates a deferred-start pass ("served by the dev-server — not supervised;
502 after a container cycle"); (b) `DeriveCloseState` *consumes* the persisted verdict so a
transient-dev service closes as **"live (transient)," not "auto-complete (delivered)"**;
(c) the `Next:` hint states the *reason* ("deploy to make this edit survive a container
cycle"). **Hard constraint:** `DeriveCloseState` reads only the persisted verdict — never live
platform/dev-server liveness (would break byte-determinism + flap closed↔open). *Trade-off:*
not prohibition — e2's user legitimately kept dev mode and `*-dev-only*` scenarios stay valid;
ZCP just refuses to *call it delivered*. Pairs with intent→mode steering at bootstrap
(`bootstrap-mode-prompt.md` reframed from iteration-frequency to durability/reachability).
Also lands Codex Missed #1: a final-response guard — an open session with pending **required**
services (now cleanly defined by #1) must not be presentable as complete.

**3 — RC-C: proactive source-of-truth signposting (ship third).**
The reactive launch gate is *correct* and now multi-runtime-aware — keep it. The defects are
(a) no early signpost and (b) no ZCP-owned "prod-deferred-pending-source-control" state (which
is why e3's agent wrote prod-deferral into its own `.claude` memory). On **explicit** prod/export
intent (not "might"), surface `git-push-setup` early and record a first-class deferred-prod
state. *Trade-off:* explicit-intent trigger avoids friction for the 90% that never launch.
Recipe-prose hostname/template-URL staleness (F4) is recipe-authoring scope → flag to Aleš.

**4 — RC-D: right-size ceremony + guidance (ship last).**
F6: skip the route-menu round-trip + redundant `includeEnvs` discover when adopt is the only
viable route (do NOT auto-adopt — pairing inference was removed for safety). F7: add a
managed-type-presence axis + task-class axis to the atom selector; split monolithic
`develop-first-deploy-env-vars.md`. (TOPO-1's `normalizeDeployDims` is the precedent for adding
a new snapshot dimension cleanly.)

### Out of scope for this plan (hand off)
- Codex Missed #3 (project-env write restarts an unrelated running service) → env-lifecycle
  workstream (`plans/zcp-env-*`).
- Stale `Variant`/`variant-prompt` comments in `workflow_export.go:18` → trivial cleanup, fold
  into whoever next touches export.

### Effort
#1 ≈ 1.5d (session-role state + scope-passthrough + denominator). #2 ≈ 2.5d (durabilityVerdict
stamp + 3 consumer surfaces + mode-prompt + final-response guard). #3 ≈ 1d (+ F4 to Aleš).
#4 ≈ 2d. Core (#1+#2) ≈ 4d.

---

## 9. Implementation fidelity check (2026-06-01, branch lifecycle-durability-completion-contract)

Per-item walk of §8 against shipped code. ✅ done · ⚠ deliberate deviation/deferral (flagged).

### RC-B (commit "RC-B: per-session service role")
- ✅ `WorkSession.Roles` (`required|deferred|out-of-scope`; absent=required → no migration break).
- ✅ denominator reads required only: `ResolvedDeployTargets`, `autoCloseGateOpen`, `AutoCloseProgressOf`, render.
- ✅ `outOfScope[]` develop input (validated: ⊆ scope, ≥1 required) flips the auto-widened stage to a reminder.
- ✅ `render.go` denominator counts required statuses; out-of-scope shown as non-blocking reminder.
- Note: the stage auto-widen in `validateDevelopScope` is KEPT (stage stays visible+required by default, per plan "default required, let the agent opt out") — not removed.

### RC-A′ (commit "RC-A': durably-delivered completion contract")
- ✅ verify annotates a deferred-start pass (`deferredStartDurabilityNote`).
- ✅ `Next: deploy` states the durability rationale (F8).
- ✅ intent→mode steering (`bootstrap-mode-prompt` reframed) + dev-server atom durability.
- ✅ close reads "live (transient), NOT durable" instead of "all services done" for a dev-mode required service.
- ⚠ **Design deviation (better, Codex-constraint-satisfying):** durability is computed in the RENDER layer from the snapshot's STABLE `(Mode, RuntimeClass)` via `IsDeferredStart` — NOT persisted as a `durabilityVerdict` field, and `DeriveCloseState` is UNTOUCHED. This fully satisfies Codex's hard constraint (no live-liveness read; byte-deterministic) without a ServiceMeta schema change. The internal `CloseReason` stays `auto-complete` (phase determinism + back-compat); the agent-facing close LINE carries the transience. Equivalent outcome, smaller blast radius.
- ⚠ **Partial — open-session-done guard (Codex Missed #1):** the blockers line surfaces "Auto-close blocked: N/M required ready, pending …" (legible not-done signal, now honest via RC-B). A HARD server-side block on the agent presenting "done" is not enforceable (ZCP can't gate the agent's prose); legibility is the lever and it is in place.
- Terse close-mode mutation response: durability note not added there (pure-disk handler lacks RuntimeClass). Covered because the **verify annotation fires on the step immediately before close**, and `action=status` (recovery primitive) renders it.

### RC-C (commit "RC-C: proactive source-of-truth signpost")
- ✅ `renderProdSourceControlSignpost` fires on explicit prod intent + git-unconfigured required service.
- ✅ reactive launch gate unchanged (now multi-runtime per LAUNCH-3/4).
- ⚠ **Deferred (flagged):** persistent ZCP-owned "prod-deferred" state — larger project-scoped-state feature; the re-firing signpost covers the core ambush-prevention. F4 (recipe-prose hostnames) = recipe-authoring scope (Aleš), not implemented per plan.

### RC-D (commit "RC-D: right-size guidance")
- ✅ F7 managed-type axis (`AxisVector.ManagedTypes` + `envelopeHasManagedType`); env cheatsheet split into 4 per-type atoms; postgres-only project gets SQL only, managed-dep-free gets none. Real MCP-cap headroom created.
- ✅ F6 adopt: route-menu round-trip already avoidable via existing `idle-adopt-entry` guidance; redundant `includeEnvs` discover softened for the adopt code-only path. Auto-adopt correctly NOT done (unsafe pairing synthesis).
- ⚠ **Deferred (flagged):** a distinct "task-class" axis (code-edit vs infra-change vs cross-service-wiring) was NOT added. The existing `deployStates`/`envelopeDeployStates` axis already branches first-deploy vs iterate (the dominant shape variance), and the new `managedTypes` axis covers dep-type variance — together these capture the bulk of F7's "irrelevant guidance" without a finer task-class cut.

### Gates
- ✅ `go test -race ./... -count=1` — all pass.
- ✅ `make lint-local` — 0 issues (incl. recipe-atom-lint + atom-template-vars custom lints).
- Eval flow validation: see §10.

---

## 10. Eval-flow validation (2026-06-01, real eval-zcp via flow-eval)

Four scenarios across the transcript flow families. All passed; **zero regressions** introduced by RC-A′..D. Every friction the retrospectives raised is pre-existing (ToolSearch namespacing, verify probes `/`, subdomain auto-enable, route-menu two-call handshake, plan-JSON nesting) — none from these changes.

| Scenario | Family | Validates | Result |
|---|---|---|---|
| `existing-standard-appdev-only-reminders` | e4 (adopt+develop) | RC-B `outOfScope`, F6 adopt-provision | ✅ agent used `outOfScope=["appstage"]`, response showed "Out of scope this session: appstage"; skipped redundant env discover |
| `api-node-postgres-classic-dev` | e2-ish (classic dev-only) | RC-A′ durability, **F2 calibration** | ✅ agent correctly picked `bootstrapMode=dev` for explicit-iteration intent — F2 reframe did NOT over-push to simple; durability caveat not flagged as friction |
| `launch-production-from-standard-pair` | launch | RC-C gate unchanged, render regression | ✅ source-control gate + git-push-setup chain intact; F6 "skip when no new wiring" visible; no render breakage |
| `recipe-laravel-minimal-standard` | e3 (recipe) | RC-D F7 split, recipe-develop integrity | ✅ "clean run — guidance unusually precise, no retries"; NO guidance-volume complaint (vs e3's original "70% irrelevant") — consistent with F7 pruning the postgres cheatsheet |

**Eval-surfaced refinement applied:** the e4 agent was unsure whether an out-of-scope hostname must also appear in `scope`; clarified the `outOfScope` schema (commit "clarify outOfScope schema").

**Recurring non-regression signal:** develop-active guidance volume ("need ~5%") still noted in two retrospectives — the deferred **task-class axis** (§9), confirmed as a real follow-up, not introduced here. F7's managed-type split removed the dep-type slice of it.

### Conclusion
RC-A′ (durability legibility), RC-B (scope-as-completion), RC-C (source-of-truth signpost), RC-D (right-sizing) are implemented, unit/integration/golden/race/lint green, and validated live across adopt+develop, classic-dev, launch-production, and recipe flows. ZCP remains fully functional across all exercised flows.
