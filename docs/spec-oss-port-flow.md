# Spec: OSS Port Flow — `zerops_port` (authoring)

How ZCP turns a piece of self-hosted OSS (Strapi, PostHog, umami, …) into a curated Zerops
recipe — **autonomously, best-effort** — by getting it running on the platform and then
publishing the result.

- **This doc** = the runtime/architecture reference: what the flow *is* and how it behaves.
- Boundary: `docs/spec-authoring-boundary.md` (the flow lives in `internal/authoring/port/`,
  gated behind `ZCP_AUTHORING`).
- Design history: `plans/archive/oss-recipe-port-flow-2026-06-09.md` (+ the software-shape and
  verification companions) — the PR #5 draft this shipped from.
- Related: `internal/ops/deploy_failure*.go` (the failure classifier this flow reads),
  `docs/spec-content-surfaces.md` (recipe content).

> **Status (2026-06-12):** shipped as the gated `zerops_port` tool in `internal/authoring/port/`
> (integrated from PR #5, reshaped to the authoring boundary — no `workflow=` value, no
> `WorkflowInput` fields, no core envelope phase). All five actions (start / iterate / harden /
> capture / status) implemented + unit/tool-tested. **Remaining: live e2e verification** —
> harden BEHAVIOR (persistence/HA) is graded from agent-reported inputs and publish is wired
> dry-run, but neither is yet proven against live Zerops; and OQ-1 (glue-repo org/token
> authority) defers the real publish.

---

## 1. What it is, and what it is NOT

OSS porting is **not** the framework-recipe activity (`zerops_recipe`). There is no source
scaffold, no dev/stage pairs, no feature showcases. It is a **port → debug → harden → capture**
loop: take someone else's software, get it to run *well* on Zerops by iterating against live
deploys + logs, then freeze the result as a recipe. The recipe scaffold engine
(`internal/authoring/recipe`) stays untouched except for the shared emit surface (§9).

**Two stages, one seam:**
- **Stage A — port & harden** (the engineering loop): get it running, prove persistence, score it.
- **Stage B — capture & publish**: turn the working deployment into a curated recipe.

The seam is a **checkpoint** by default (user decision 2026-06-09): Stage A runs to a working,
scored deployment and stops with an honest fit report; capture is triggered separately so a human
can inspect a gnarly port before publishing.

**Trigger routing** (the gated AGENTS.md authoring block): "create umami recipe" / "port strapi" /
"make a recipe for posthog" mean THIS flow when the subject is third-party OSS — `zerops_recipe`
would wrongly author it from scratch as a framework showcase.

---

## 2. Architecture

- **A self-registering gated MCP tool** — `zerops_port` (`port.Register`, mirroring
  `recipe.Register`), registered by `internal/server` ONLY inside the `ZCP_AUTHORING` gate.
  It owns its whole dispatch (start / iterate / harden / capture / status), its own input
  struct (`port.PortInput`), and its own in-band JSON envelopes. It is NOT a `workflow=`
  value, carries NO fields on `WorkflowInput`, and never appears in the core lifecycle
  envelope — core knows nothing about it (boundary law L1).
- **The loop is AGENT-DRIVEN across tool turns — not an engine-internal coroutine.** Each iteration
  is one agent turn: the agent runs the deploy via the existing tools (`zerops_deploy`/`zerops_import`/
  `zerops_env`/…), observes the result, then calls the port `iterate` action; the handler derives the
  next fix and records state. Building an I/O-suspending interpreter would violate the pinned
  *stateless STDIO tools* invariant. "Fully autonomous best-effort" is honored at the **session**
  level (no human approval gate; the loop self-terminates on rubric + caps), not inside the engine.
- **Layering:** the whole flow lives in `internal/authoring/port/` — engine (recon, fix-class,
  rubric, fit-ceiling, harden, progress, escalate), session sidecar, and the tool handlers.
  Its core imports are exactly the L2 allowlist subset it needs: `schema` (managed-type
  existence), `topology` (classification + FailureClass + Recovery), `platform` (error codes),
  `sync` (publish config, C5) + the authoring-internal `recipe` (emit verbs) and `publish`
  (repo lifecycle). NOT `workflow`, NOT `tools`, NOT `ops`.

---

## 3. State model

- **`PortSession`** (`internal/authoring/port/session.go`) — per-PID sidecar at
  `.zcp/state/port/{pid}.json`, the authoring-owned state namespace (boundary contract C3,
  pinned by `TestAuthoringBoundary_StateNamespaces`). It is **standalone** — it records its
  own loop attempts and terminal close, and never reads or writes the core
  `.zcp/state/{work,services,…}` namespaces. Identity + time are two separate fields:
  - `StartTime` — the (pid,startTime) staleness guard: an opaque process-instance token; a
    recycled PID can't inherit a dead predecessor's session (`LoadPortSession` treats a
    foreign token as not-found; empty trusts the bare PID).
  - `CreatedAt` — RFC3339 session creation, the **wall-budget anchor**. (The PR draft anchored
    the budget on the process-identity field, which is not parseable time on Linux — the
    terminator could never fire; the split fixes that.)
- **`PortPlan`** — recon output: `{Target, Acquisition AcquisitionStrategy, Band FeasibilityBand,
  Runtimes, Dependencies []PortDependency, PrebuiltURL, Constraints}`.
- **`PortAttempt`** — `{Iteration, RecordedAt, Hostname, Class FailureClass, Signals[], FixKind,
  Escalate, Succeeded}`. **Failure signals live here, NOT on the core `DeployAttempt`** (a
  deliberate low-blast-radius choice — `DeployAttempt` persists only the coarse `FailureClass`
  category; signal-level fix-class dispatch needs the signals, so they're threaded per-turn and
  stored port-side). `Attempts` is the substrate for stall detection.

---

## 4. Stage A — port & harden

### A0 — Recon / classify (pure, zero-deploy) — `recon.go`
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

### A1 — Deploy-debug loop (agent-driven) — `fixclass.go` + the `iterate` handler
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
edit is unavoidable (`importOverride=true` on the iterate input).

### A2 — Harden — `harden.go` + the `harden` handler
After the app builds/boots/serves: write persistence sentinels (write → redeploy → re-read) to
prove data lives on managed DB / object-storage / shared-storage (never the ephemeral container FS);
inject `readinessCheck`/`healthCheck`; probe HA by scaling ≥2 containers + managed HA-mode. The
agent runs the probes via the existing tools and reports observations in the `rubric` input;
the handler GRADES them into C5/C6 — it never re-runs anything.

---

## 5. Loop mechanics (self-terminating) — `progress.go` + `escalate.go`

Three independent terminators; any fires Stage A as a **graceful scored stop** (never spin):

1. **Two-counter stall detection.** `classStallStreak` keys on the `FailureClass` **category**
   (coarse — survives `Signals` variation within one root cause). `phaseStallStreak` keys on
   fix-class **phase** non-advancement (build→start→serve); it takes a `progressRose` seam the
   harden turn feeds with rubric tier-rise.
2. **Iteration cap per band** — EASY 4 / MEDIUM 8 / HARD 12 → stamps the session's terminal
   close (`closeReasonIterationCap`; idempotency guarded on `CloseReason`, never a raw `ClosedAt`
   read). Re-budgeted (`RebudgetOrigin`) when a T1 escalation fires so a late
   strategy switch isn't starved.
3. **Wall budget** — EASY 45m / MEDIUM 60m / HARD 90m from `CreatedAt` (time injected for
   testability). Poll primitives already bound each call, so the budget bounds the *number* of
   polls, not a single hang.

**Strategy escalation ladder** (typed triggers, never raw retry count): **T0** stay; **T1**
`source-build`→`prebuilt-binary` when `classStallStreak≥3` on build (or the build-OOM flag) AND a
`PrebuiltURL` exists; **T2** bail-with-ceiling. `credential`/`network` never escalate strategy.

---

## 6. Status recovery — `status_recovery.go`

The core `zerops_workflow action="status"` knows nothing about the port flow (authoring is
invisible to core), so `zerops_port action="status"` is the flow's own compaction-recovery
primitive. `BuildPortActiveRecovery(*PortSession)` builds the tool-owned envelope
(fresh-recon / mid-loop / last-succeeded / escalation-flagged) with a copy-pasteable
`nextCall`. Pure + deterministic for byte-stable recovery.

---

## 7. Verification rubric → FitCeiling — `rubric.go` + `fitceiling.go`

Stage A scores rather than passes/fails — graded checks roll up to the highest honored deployment
tier:

| Check | Verified by | Honors |
|---|---|---|
| C1 Builds | agent-observed build terminal + warnings | gate |
| C2 Boots (STABLE) | process status + runtime logs after a stability hold (catches the 200ms-exit / crash-loop false-positive) | gate |
| C3 Serves HTTP | core `zerops_verify` http_root (agent-run) | Tier 0/1 |
| C4 Core flow | agent-authored probe (the loop can't understand a foreign app's health endpoint) | Tier 2/3 |
| C5 Persists across redeploy | harden sentinel | Tier 4 |
| C6 HA-capable | scale ≥2 + managed HA-mode (throughput ≠ HA replication, kept distinct) | Tier 5 |

The **`FitCeiling`** is the honest report: what runs, what doesn't, achievable HA, honored tiers
(each excluded tier carries a reason). A tier ships only if its rubric prerequisites are met
("don't ship a tier whose guide you can't honor"). `haDeps` (the managed deps the agent MEASURED
running HA) is the single source of truth for both the C6 grade and the emitted per-service HA
type-variant (`:ha`/`:single`) — an unmeasured dep is never force-promoted to HA.

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
  HARD convergence is iteration-heavy and may need human-seeded gotchas.

Bands: **EASY** (Strapi/Ghost/most CMS) → fully autonomous, complete recipe. **HARD** (PostHog) →
durable, production-scale, HA-capable, but iteration-heavy + may need seeded hints.

---

## 9. Stage B — capture & publish — `capture.go`

A curated OSS recipe is shaped **exactly like a framework recipe** — there is nothing OSS-special
about the *packaging*:

- **App / glue → `zerops-recipe-apps/<slug>`** (the `buildFromGit` target: glue `zerops.yaml` +
  build scripts, the `recipe-posthog` shape). Via `publish.CreateRecipeRepo` + `publish.PushAppSource`.
- **Recipe envs + metadata → `zeropsio/recipes`** — `environments/<order> — <Name>/{import.yaml,
  README.md}` + Strapi entry (same fields as a framework recipe), via `publish.Recipe`.
- **Rich content = fragments** — `description`/`features`/`takeover-guide`/`knowledge-base`
  authored from the measured FitCeiling evidence as paired `#ZEROPS_EXTRACT_START:NAME#` /
  `#ZEROPS_EXTRACT_END:NAME#` markers in the app README, the same machinery framework recipes use
  (`recipe.SubstituteFragmentMarkers`).
- The **only** `authoring/recipe` surface Stage B needed was the `Plan.GlueRepoURL` buildFromGit
  override (D6 — both emit sites, runtime AND the `ServiceKindUtility` branch, canonicalized via
  `topology.CanonicalRepoURL`) + `Service.ModeMeasured` (measured per-service HA-variant emission —
  `ManagedServiceModeForTier` is the single mode-resolution owner; its resolved mode is converted
  to the `:ha`/`:single` type variant by `variantForMode`→`topology.VariantForHA`).
- The emitted output lands under `.zcp/state/port-recipes/<slug>` (the second authoring-owned
  state namespace) so it never pollutes the repo; the capture handler defers the publish
  (emit-only) while `glueRepo.buildFromGitReady` is false (OQ-1), and `publishDryRun=true`
  exercises the whole publish path with no GitHub write.

> **Channel distinction (do not conflate):** the **`.zerops-recipe/` folder** (with its own per-env
> folders + fragment markers in a *single public repo* the frontend live-fetches via
> `/api/recipe/info`) is the **self-service** channel — e.g. `fxck/zrno-shop`. **Curated recipes do
> NOT use a `.zerops-recipe/` folder**; they split across `zerops-recipe-apps` (app/glue) +
> `zeropsio/recipes` (envs + Strapi). The port flow targets the **curated** channel.

---

## 10. End-to-end (operator's view)

1. `zerops_port action="start"` with a target descriptor → recon → `PortPlan` +
   band (zero deploy). Bails honestly only on K8s-runtime-only software.
2. The agent runs the deploy-debug loop: deploy (existing tools) → `action="iterate"` with the
   observed failure → handler derives the next fix-class / escalates / stops. Self-terminates on
   stall, cap, or budget.
3. Harden + score → `action="harden"` (plan first, then rubric) → `FitCeiling`. **Checkpoint:**
   Stage A stops here with the honest report.
4. Capture & publish: `action="capture"` — app → `zerops-recipe-apps`, recipe → `zeropsio/recipes`.

`action="status"` recovers the loop after compaction at any point.

---

## 11. Open questions

Glue-repo org/token write-authority for autonomous create (OQ-1 — `.sync.yaml` pins
`zerops-recipe-apps` / `zeropsio/recipes`, ambient `gh auth`; capture defers publish until
`buildFromGitReady`); whether HARD-band recon should accept an OSS-knowledge seeding input;
ServiceMeta/subdomain decision at first import; the C4 probe's structured contract; live e2e
verification of harden behavior + a real (non-dry-run) publish.
