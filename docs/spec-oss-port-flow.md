# Spec: OSS Port Flow

How ZCP turns a piece of self-hosted OSS (Strapi, PostHog, …) into a curated Zerops
recipe — **autonomously, best-effort** — by getting it running on the platform and then
publishing the result.

- **Plan / build roadmap** (phased, with verification history): `plans/oss-recipe-port-flow-2026-06-09.md`.
- **This doc** = the runtime/architecture reference: what the flow *is* and how it behaves.
- Related: `docs/spec-workflows.md` (peer workflows), `docs/spec-work-session.md` (the wrapped
  session), `docs/spec-content-surfaces.md` (recipe content), `internal/ops/deploy_failure*.go`
  (the failure classifier this flow reads).

> **Status (2026-06-10):** Phases 0–4 implemented + committed (recon; agent-driven loop +
> fix-class; termination/escalation; rubric + FitCeiling + harden GRADING; capture + publish wiring
> — D6 buildFromGit override, PortSession→Plan, honored-tier emit, dry-run sync publish). Phase 5
> (HARD-band honesty + PostHog fixture) and **live e2e verification of harden/publish** remain. "Designed"
> items below are marked; harden BEHAVIOR (persistence/HA) is graded from inputs but not yet live-verified.

---

## 1. What it is, and what it is NOT

OSS porting is **not** the framework-recipe activity. There is no source scaffold, no dev/stage
pairs, no feature showcases. It is a **port → debug → harden → capture** loop: take someone
else's software, get it to run *well* on Zerops by iterating against live deploys + logs, then
freeze the result as a recipe. It therefore lives on the **deploy-debug ops layer** (where
`develop` lives), **not** in the `internal/recipe` scaffold engine — which stays completely
untouched (zero regression to framework recipes).

**Two stages, one seam:**
- **Stage A — port & harden** (the engineering loop): get it running, prove persistence, score it.
- **Stage B — capture & publish** (designed): turn the working deployment into a curated recipe.

The seam is a **checkpoint** by default (user decision 2026-06-09): Stage A runs to a working,
scored deployment and stops with an honest fit report; capture is triggered separately so a human
can inspect a gnarly port before publishing.

---

## 2. Architecture

- A **new `port` workflow, peer to `develop`** — `WorkflowPort` (`internal/workflow/port_recon.go`),
  phase `PhasePortActive` (`internal/workflow/envelope.go`). It rides the **empty-Plan fall-through**
  in `build_plan.go` exactly like `export` / `launch-production` (it drives no recipe `Plan`; its
  handlers emit their own guidance). Because of that it needs its own status recovery (see §6).
- **The loop is AGENT-DRIVEN across tool turns — not an engine-internal coroutine.** Each iteration
  is one agent turn: the agent runs the deploy via the existing tools (`zerops_deploy`/`zerops_import`/
  `zerops_env`/…), observes the result, then calls the port `iterate` action; the handler derives the
  next fix and records state. Building an I/O-suspending interpreter would violate the pinned
  *stateless STDIO tools* invariant. "Fully autonomous best-effort" is honored at the **session**
  level (no human approval gate; the loop self-terminates on rubric + caps), not inside the engine.
- **Layering / depguard:** all port logic lives in `internal/workflow/port_*.go` (state, recon,
  fix-class, progress, escalate, recovery) + `internal/tools/workflow_port*.go` (the handler
  boundary). `internal/workflow` imports no `ops`/`tools`/`recipe`; `topology` stays stdlib-only.
  Stage B's emit/publish (designed) lives in `internal/tools` (tools→recipe is permitted).

---

## 3. State model

- **`PortSession`** (`internal/workflow/port_session.go`) — per-PID sidecar at
  `.zcp/state/port/{pid}.json`, mirroring `work_session.go` conventions. It **wraps a `WorkSession`**
  (where deploy/verify attempts record) and adds port-owned state: `Plan PortPlan`, `Iteration`,
  `RebudgetOrigin`, and `Attempts []PortAttempt`.
- **`PortPlan`** — recon output: `{Target, Acquisition AcquisitionStrategy, Band FeasibilityBand,
  Runtimes, Dependencies []PortDependency, PrebuiltURL, Constraints}`.
- **`PortAttempt`** — `{Iteration, RecordedAt, Hostname, Class FailureClass, Signals[], FixKind,
  Escalate, Succeeded}`. **Failure signals live here, NOT on the shared `DeployAttempt`** (a
  deliberate low-blast-radius choice — `DeployAttempt` persists only the coarse `FailureClass`
  category; signal-level fix-class dispatch needs the signals, so they're threaded per-turn and
  stored port-side). `Attempts` is the substrate for stall detection.

---

## 4. Stage A — port & harden

### A0 — Recon / classify (pure, zero-deploy) — `port_recon.go`
Turns an **agent-supplied target descriptor** (target name, acquisition hint, declared
dependencies + runtimes) into a `PortPlan`. Recon *classifies*; the agent *researches* the OSS.

- **Acquisition ladder:** source repo → `source-build`; binary URL only → `prebuilt-binary`;
  **image-only → `crane-image-lift`** (extract the image rootfs onto a stock base runtime in
  `run.prepareCommands` — see §8; this is IN-band, NOT a bail); needs Kubernetes *runtime*
  orchestration primitives → `bail`.
- **Dependency mapping:** each declared dep → managed catalog via `internal/schema` (`HasServiceType`
  / `ManagedBaseNames`) + `internal/topology` predicates. `managed` (env-ref injected) / `self-run`
  (needs its own glue buildFromGit, raises band) / `constraint`. ClickHouse + Kafka ARE managed.
- **Band:** `easy` (source/crane + all managed) / `medium` (+1 self-run) / `hard` (many self-run /
  many runtimes / cross-service init ordering) / `bail`. The band is an *estimate*; the real ceiling
  is **measured** by the rubric (§5/§7).

### A1 — Deploy-debug loop (agent-driven) — `port_fixclass.go` + `workflow_port_iterate.go`
The agent deploys; the handler reads the result's `FailureClassification` **FIRST** (never parses
logs to choose) and derives a **fix-class**:

| FailureClass + signal | Fix-class |
|---|---|
| build + command-not-found | add `prepareCommands` install |
| build + oom-killed | **escalation trigger, NOT a fix** (build resources aren't import-tunable) |
| start + db-connection-refused | wire `${dep_*}` refs (+ add managed dep) |
| start + missing-env | `EnvSet` |
| start + migration-failed | move to `run.initCommands` + `zsc execOnce ${appVersionId} --retryUntilSuccessful` |
| config | fix the glue `zerops.yaml`, re-validate live |
| credential | chain to `git-push-setup` |
| network | retry |

Fix-class guidance **prefers glue-`zerops.yaml` edits over `import.yaml` edits**: an `import.yaml`
edit to an *existing* hostname trips the import-override gate (`ErrDiagnosisRequired` from iteration
2, and `override=true` wipes container/env state), so the guidance flags that tax when an import
edit is unavoidable.

### A2 — Harden (DESIGNED, Phase 3)
After the app builds/boots/serves: write persistence sentinels (write → redeploy → re-read) to
prove data lives on managed DB / object-storage / shared-storage (never the ephemeral container FS);
inject `readinessCheck`/`healthCheck`; probe HA by scaling ≥2 containers + managed HA-mode. Produces
the C5/C6 rubric grades.

---

## 5. Loop mechanics (self-terminating) — `port_progress.go` + `port_escalate.go`

Three independent terminators; any fires Stage A as a **graceful scored stop** (never spin):

1. **Two-counter stall detection.** `classStallStreak` keys on the `FailureClass` **category**
   (coarse — survives `Signals` variation within one root cause). `phaseStallStreak` keys on
   fix-class **phase** non-advancement (build→start→serve); it takes a `progressRose` seam that
   Phase 3 feeds with rubric tier-rise.
2. **Iteration cap per band** — EASY 4 / MEDIUM 8 / HARD 12 → closes the wrapped session with the
   existing `CloseReasonIterationCap` (idempotency guarded on `CloseReason`, never a raw `ClosedAt`
   read — P5 invariant). Re-budgeted (`RebudgetOrigin`) when a T1 escalation fires so a late
   strategy switch isn't starved.
3. **Wall budget** — EASY 45m / HARD 90m (time injected for testability). Poll primitives already
   bound each call, so the budget bounds the *number* of polls, not a single hang.

**Strategy escalation ladder** (typed triggers, never raw retry count): **T0** stay; **T1**
`source-build`→`prebuilt-binary` when `classStallStreak≥3` on build (or the build-OOM flag) AND a
`PrebuiltURL` exists; **T2** bail-with-ceiling. `credential`/`network` never escalate strategy.

---

## 6. Status recovery — `port_status_recovery.go`

`PhasePortActive` carries an empty `Plan`, so `action="status"` can't reconstruct from a recipe
plan (the launch-production situation). `BuildPortActiveRecovery(*PortSession)` builds a lifecycle
envelope from the `PortSession` (fresh-recon / mid-loop / last-succeeded / escalation-flagged), so
the loop survives context compaction. Pure + deterministic for byte-stable recovery.

---

## 7. Verification rubric → FitCeiling (DESIGNED, Phase 3)

Stage A scores rather than passes/fails — graded checks roll up to the highest honored deployment
tier:

| Check | Verified by | Honors |
|---|---|---|
| C1 Builds | PollBuild terminal + warnings | gate |
| C2 Boots (STABLE) | process status + runtime logs after a stability hold (catches the 200ms-exit / crash-loop false-positive) | gate |
| C3 Serves HTTP | `Verify` http_root | Tier 0/1 |
| C4 Core flow | agent-authored probe (the loop can't understand a foreign app's health endpoint) | Tier 2/3 |
| C5 Persists across redeploy | harden sentinel | Tier 4 |
| C6 HA-capable | scale ≥2 + managed HA-mode (throughput ≠ HA replication, kept distinct) | Tier 5 |

The **`FitCeiling`** is the honest report: what runs, what doesn't, achievable HA, honored tiers
(each excluded tier carries a reason). A tier ships only if its rubric prerequisites are met
("don't ship a tier whose guide you can't honor").

---

## 8. Feasibility — what the platform can host

The platform ceiling is **much higher than first assumed** (proven by `fxck/recipe-posthog`):

- **Image-only is portable via crane-lift.** `crane export <image>:latest` extracts the image
  filesystem onto a stock base runtime (`ubuntu/python@3.12`, `ubuntu/nodejs@24`), repoints
  hardcoded paths/venv, runs the binaries directly. No native image-deploy field needed.
- **All of PostHog's deps are managed**, and **ClickHouse runs HA** (mandatory for its `ON CLUSTER`
  DDL). Cross-service init ordering is handled with `zsc execOnce --retryUntilSuccessful` +
  `zsc scale ram max` (autoscaler OOM-race dodge).
- **The only true bail** is software needing Kubernetes *runtime* orchestration (sidecars-as-pods,
  service mesh) inexpressible as `prepareCommands`/`initCommands`.
- **The real frontier for *autonomous* porting is knowledge depth, not feasibility.** HARD targets
  (PostHog) need deep OSS-internals discoveries — some with *no failure signal at all* (a Kafka SASL
  producer hanging on missing per-producer-prefix env). The agent-driven loop is the right vehicle;
  HARD convergence is iteration-heavy and may need human-seeded gotchas. See
  `[[reference_oss_port_crane_lift]]` and plan §12.

Bands: **EASY** (Strapi/Ghost/most CMS) → fully autonomous, complete recipe. **HARD** (PostHog) →
durable, production-scale, HA-capable, but iteration-heavy + may need seeded hints.

---

## 9. Stage B — capture & publish (DESIGNED, Phase 4)

A curated OSS recipe is shaped **exactly like a framework recipe** — there is nothing OSS-special
about the *packaging*:

- **App / glue → `zerops-recipe-apps/<slug>`** (the `buildFromGit` target: glue `zerops.yaml` +
  build scripts, the `recipe-posthog` shape). Via `zcp sync recipe create-repo` + `push-app`.
- **Recipe envs + metadata → `zeropsio/recipes`** — `environments/<order> — <Name>/{import.yaml,
  README.md}` + Strapi entry (same fields as a framework recipe), via `zcp sync recipe publish
  <slug> <dir> --software <name> --name --desc --tags --cover`.
- **Rich content = fragments** — `description`/`features`/`takeover-guide`/`knowledge-base` (+
  per-service `integration-guide`) authored as `#ZEROPS_EXTRACT_START:NAME#` markers in the repo
  READMEs, the same machinery framework recipes use (the `internal/recipe` emit verbs +
  recipe-level fragment surfaces). The marker form is the paired `#ZEROPS_EXTRACT_START:NAME#` /
  `#ZEROPS_EXTRACT_END:NAME#` (verified accepted by the backend `/api/recipe/info` parser).
- The **only** `internal/recipe` change Stage B needs is a `buildFromGit` override so emitted
  runtime + utility services point at `zerops-recipe-apps/<slug>` (both emit sites — runtime AND
  the `ServiceKindUtility` branch).

> **Channel distinction (do not conflate):** the **`.zerops-recipe/` folder** (with its own per-env
> folders + fragment markers in a *single public repo* the frontend live-fetches via
> `/api/recipe/info`) is the **self-service** channel — e.g. `fxck/zrno-shop`. **Curated recipes do
> NOT use a `.zerops-recipe/` folder**; they split across `zerops-recipe-apps` (app/glue) +
> `zeropsio/recipes` (envs + Strapi). The port flow targets the **curated** channel.

---

## 10. End-to-end (operator's view)

1. `zerops_workflow workflow=port action=start` with a target descriptor → recon → `PortPlan` +
   band (zero deploy). Bails honestly only on K8s-runtime-only software.
2. The agent runs the deploy-debug loop: deploy (existing tools) → `action=iterate` with the
   observed failure → handler derives the next fix-class / escalates / stops. Self-terminates on
   stall, cap, or budget.
3. Harden + score → `FitCeiling` (DESIGNED). **Checkpoint:** Stage A stops here with the honest
   report.
4. Capture & publish (DESIGNED): app → `zerops-recipe-apps`, recipe → `zeropsio/recipes`.

`action=status` recovers the loop after compaction at any point.

---

## 11. Open questions (tracked in the plan)

Glue-repo org/token write-authority for autonomous create (`.sync.yaml` pins `zerops-recipe-apps`
/ `zeropsio/recipes`, ambient `gh auth`); whether HARD-band recon should accept an OSS-knowledge
seeding input; ServiceMeta/subdomain decision at first import; the C4 probe's structured contract.
