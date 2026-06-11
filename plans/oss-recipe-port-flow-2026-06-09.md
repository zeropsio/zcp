# Implementation Plan: Autonomous OSS Port Flow (Strapi / PostHog → recipe)

**Date**: 2026-06-09
**Task**: Design the *inner flow* that creates proper OSS recipes — the raw port-and-debug
activity of getting self-hosted software (Strapi, PostHog) running on Zerops "as best we can,"
fully autonomously, then capturing it as a `shape: software` recipe.
**Method**: team-design workflow, 4 phases (3 ground readers → design → adversarial feasibility →
synthesis), 6 agents, grounded in the live codebase + `zerops-docs`.
**Relationship to** `plans/oss-recipe-software-shape-2026-06-09.md`: that plan extends the
`internal/recipe` *scaffold engine* and is now **demoted to a fallback**. This plan is the
primary approach. The only parts of that plan this one keeps are its **D4** (recipe-level
fragments) and **D6** (`buildFromGit` override) — they are the additive recipe changes Stage B
needs; the phase-engine extension is superseded.

---

## 0. What this is (and what it is NOT)

OSS recipe creation is **not** the framework recipe activity. No source scaffold, no dev/stage
pairs, no feature showcases. It is a **port → debug → harden → capture** loop: take foreign
software, get it to run well on Zerops by iterating against live deploys + logs, then freeze the
result as a recipe. It lives on the **deploy-debug ops layer** (where `develop` lives), not in the
scaffold engine.

**Autonomy** = fully autonomous best-effort: no human approval gate; the agent ports as far as it
can and emits an **honest fit-ceiling** (what runs, what doesn't, what HA is achievable).

---

## 1. Architecture (confirmed, with one forced correction)

A **new `port` workflow, peer to `develop`**, on the deploy-debug ops layer, two internal stages
(**A** port-and-harden, **B** capture-to-recipe). **Not** an extension of `internal/recipe`'s
scaffold engine.

**Verified precedent:** `export` and `launch-production` are already peer workflows whose phases
do not drive a `Plan` — `build_plan.go:41-46` falls through to an empty plan and the handlers emit
their own guidance. A `PhasePortActive` slots into that **same fall-through case** — not greenfield.

**The correction the codebase forces (decisive):** the loop is **agent-driven across tool turns**,
**not** an engine-internal autonomous coroutine. Every ZCP loop today (develop, recipe iterate)
works this way: the handler computes state + next-action guidance, the **agent (LLM)** calls the
next ops tool, the handler records and re-derives. Building an I/O-suspending interpreter would be
~2–3K LoC of resumable machinery **against the pinned "stateless STDIO tools" invariant**. So:

> "Fully autonomous best-effort" is honored at the **session** level (no human approval gate; the
> agent self-terminates on the rubric + caps), **not** at the engine-internal level. The handler
> owns deterministic fix-class dispatch + termination math; the agent owns the semantic authoring
> (the OSS-specific glue `zerops.yaml`, the core-flow probe) it does each turn.

---

## 2. Platform feasibility — what's VERIFIED (changes the ambition)

- **Managed catalog is richer than assumed.** `clickhouse@25.3` and `kafka@{3.8,3.9}` **are
  managed** (alongside postgresql@{14,16,17,18}, valkey, keydb, elasticsearch, meilisearch,
  qdrant, typesense, nats, rabbitmq@3.9, object-storage, shared-storage). So **PostHog's deps all
  have managed equivalents** — the blocker is *not* missing services.
- **Image-only OSS is portable via crane-lift — NOT a bail** (corrected by `fxck/recipe-posthog`,
  see §12). The import schema has no native image-deploy property, but you don't need one: a
  `run.prepareCommands` step does `crane export <image>:latest` to extract the image *filesystem*
  onto a stock base runtime (`ubuntu/python@3.12`, `ubuntu/nodejs@24`), repoints hardcoded paths,
  and runs the binaries directly. So the acquisition ladder is **source-build → prebuilt-binary →
  crane image-lift**, and image-only software is in-band. The true bail is narrower: software that
  needs **Kubernetes *runtime* orchestration semantics** (sidecars-as-pods, init-container ordering,
  service mesh) that can't be expressed as `prepareCommands`/`initCommands`.
- **No cross-service RUNTIME readiness barrier.** Zerops has `priority` (creation order,
  import schema) + per-service `readinessCheck`/`healthCheck`, but **no "wait for service A
  healthy before B starts" primitive.** The standard, proven workaround (PostHog, §12) is
  `zsc execOnce <key> --retryUntilSuccessful` for cross-service init steps + `zsc scale ram max`
  to dodge the startup OOM race — a known idiom, not a hard cap. It relies on the OSS tolerating
  eventual-consistency startup (most do).

---

## 3. The inner flow (stages)

| Stage | Purpose | Key primitives |
|---|---|---|
| **A0 Recon** (no deploy) | OSS id → `PortPlan`: acquisition strategy, dep→managed map, band, first candidate `import.yaml` + glue `zerops.yaml`. An *estimate*, not the ceiling. | `schema.Cache` catalog, `topology` predicates. **NEW** classifier. |
| **A1 Deploy-debug loop** (agent-driven) | Get it to run: deploy → poll → read `FailureClassification` (FIRST, never parse logs to choose) → derive fix-class → agent applies → record → re-derive. | **REUSED** `ops.Import/EnvSet/DeploySSH/PollBuild/PollProcess/FetchBuildLogs/FetchRuntimeLogs/Events/ExecSSH/Start/Restart`. **NEW** fix-class table. |
| **A2 Harden** (after C1–C3 pass) | Prove "you own the data": persistence sentinels (write→redeploy→re-read), readiness/health injection, subdomain, HA scale probe + per-tier fitness. | **REUSED** `ExecSSH`, `Verify/VerifyAll`, `ScaleParams`. **NEW** sentinel + tier-fitness recorder. |
| **B0 Handoff** | Snapshot working deployment + `FitCeiling` → `port-handoff.json` (topology, fragment bodies authored from loop notes, glue-repo URL+SHA, honored tiers + reasons). | **NEW** manifest writer. |
| **B1 Capture** = the existing **framework-recipe capture**, reused wholesale (§12.6) | Emit the recipe output dir `environments/<N> — <Name>/{import.yaml, README.md}` from the working deployment, AND the **fragments** (`#ZEROPS_EXTRACT_START:NAME#` markers — `description`/`features`/`takeover-guide`/`knowledge-base` + per-service `integration-guide`) into the repo READMEs, exactly as a framework recipe does. **The only thing NOT produced is the `.zerops-recipe/` folder** (that's the self-service channel). | **REUSED** `EmitDeliverableYAML`, `Tiers/TierAt`, `substituteFragmentMarkers`, `AssembleRootREADME` + the **D4 recipe-level fragment surfaces** (same as framework recipes — back ON the path). |
| **B2 Publish** | App/glue → `zerops-recipe-apps/<slug>` via `zcp sync recipe create-repo` + `push-app`. Recipe envs + metadata → `zeropsio/recipes` via `zcp sync recipe publish <slug> <dir> --software <name> --name --desc --tags --cover`; Strapi entry is the **same shape as a framework recipe**. | **REUSED** `sync.PublishRecipe`/`PushAppSource` + `internal/sync/github.go`. **NEW**: the buildFromGit override (D6) so runtime/utility services point at `zerops-recipe-apps/<slug>`. |
| **B2 Glue-repo commit** | Commit the agent-authored glue `zerops.yaml` to a repo ZCP controls so `buildFromGit` resolves. | **REUSED** `zcp sync recipe create-repo/publish` (non-interactive, `cmd/zcp/sync.go:218`) + `internal/sync/github.go`. |

### The fix-class dispatch (A1 core)
`FailureClass ∈ {build,start,verify,network,config,credential,other}` + signals →
deterministic next action: build+command-not-found→add `prepareCommands` install;
**build+oom-killed→T1 escalation/bail trigger, NOT a fix** (build-container resources are not
import-tunable — `build.verticalAutoscaling` is schema-unsupported, `validate_test.go:47`);
start+db-connection-refused→wire `${db_*}` + add managed dep; start+missing-env
→`EnvSet`; start+migration-failed→move to `run.initCommands` + `zsc execOnce ${appVersionId}`;
config→fix glue yaml + re-validate live; credential→chain to `git-push-setup`; network→retry.

> **Import-override gate (verification §4) is in the loop's path.** Deploy-level fixes (the glue
> `zerops.yaml`) are ungated, but any `import.yaml` fix to an *existing* hostname (resources, type
> version, mounts, `startWithoutCode`) needs `override=true`, and `gateOverrideOnFailedHistory`
> (`internal/tools/import.go:111-216`) fires `ErrDiagnosisRequired` from iteration 2 onward — the
> port loop's exact state. The R6 `retryCall` makes it a two-call dance (not a deadlock), but each
> gated import **burns an iteration** (EASY cap = 4) and override **wipes prior container/env
> state** (full redeploy + env re-set). The fix-class table prefers glue-`zerops.yaml` edits over
> `import.yaml` edits where possible, and the iteration budget must account for the override tax.

---

## 4. Autonomous loop mechanics (self-terminating, never spin)

Three independent terminators; any fires Stage A as a **graceful scored stop**:

1. **Two-counter stall detection** (designed to survive the "signal varies per fix, streak resets"
   failure mode): `classStallStreak` keys on **FailureClass category** (coarse — survives signal
   variation within one root cause); `phaseStallStreak` keys on **`failedPhase` non-advancement +
   honored-tier non-rise**. A loop stuck on C3 with C1=C2=1 trips `phaseStall` because the phase
   stays at serve and the tier does not rise. Substrate is the existing per-hostname `Deploys[]`
   history (capped 10). **Correction (verification §3):** the coarse `classStallStreak` is
   zero-storage, but `DeployAttempt` (`work_session.go:76-82`) persists only the `FailureClass`
   *category*, **not** `Signals[]` (those live on the live `ops.DeployResult.FailureClassification`).
   Since the fix-class table dispatches on signal IDs, Phase 1 must add a persisted `Signals` field
   or thread them per-turn from the handler — signal-level dispatch is **not** zero-storage.
2. **Iteration cap per band** (EASY 4 / MEDIUM 8 / HARD 12) → `CloseReasonIterationCap` (existing
   terminal close). Re-budgeted on a T1 escalation so a late strategy switch isn't starved.
3. **Wall budget** (45m EASY / 90m HARD); poll primitives already bound each call, so the budget
   bounds the *number* of polls, not a single hang.

**Strategy escalation ladder** (typed triggers, never raw retry count): **T0** stay (new class +
budget left → apply fix); **T1** source→prebuilt (classStall≥3 on build AND a prebuilt URL exists);
**T2** bail-with-ceiling (no prebuilt, or post-escalation phaseStall, or a proven platform
constraint). `credential`/`network` never escalate strategy.

---

## 5. Verification rubric (scores, doesn't pass/fail) → FitCeiling

| Check | How verified | Roll-up |
|---|---|---|
| **C1 Builds** | `PollBuild` terminal + warnings | gate |
| **C2 Boots (STABLE)** | `GetProcessStatus` + runtime logs **after a stability hold** — ACTIVE-then-exit / crash-loop grades 1, not 2 | gate |
| **C3 Serves HTTP** | `Verify` http_root + `PassedForLifecycle` | → Tier 0/1 honorable |
| **C4 Core flow** | **agent-authored** ExecSSH probe + agent-browser render (handler requests at harden gate; loop can't understand a foreign health endpoint) | → Tier 2/3 |
| **C5 Persists across redeploy** | harden sentinel write→redeploy→re-read per claim | → Tier 4 |
| **C6 HA-capable** | `ScaleParams` ≥2 + `VerifyAll` + managed-HA catalog; **throughput ≠ HA replication** kept distinct | → Tier 5 |

`FitCeiling` = highest-honored-tier projection up the `tiers.go` ladder, **measured not assumed**;
recon band is only the estimate and the report shows both. **Contract: ship a tier only if its
rubric prerequisites are met** ("don't ship a tier whose guide you can't honor"). `C5=2,C6=1` ⇒
ship 5 tiers, mark Tier 5 not-honored with the constraint string.

---

## 6. Feasibility bands + the two worked targets

- **EASY** — source + all deps managed, no self-run, no cross-service ordering. **Strapi**
  (Node + Postgres + object-storage): loop converges in ~2 iters (env-ref wiring), all six tiers
  honored, complete shippable recipe. **Autonomous flow fully succeeds.**
- **MEDIUM** — source + exactly one self-run service that boots independently. Tiers 0–4 typical.
- **HARD** — image-lift and/or many runtimes + cross-service init ordering + deep OSS-internals
  knowledge. **PostHog is the proof it's FEASIBLE (corrected, §12)**: crane-lifted from
  `posthog/posthog` + `posthog/posthog-node`, **9 runtime services** + Postgres×2 + **ClickHouse in
  HA mode** (mandatory for its `ON CLUSTER` DDL) + Kafka + Valkey + object-storage, init ordering
  via `zsc execOnce --retryUntilSuccessful`, OOM-race dodged via `zsc scale ram max`. It **runs at
  full HA** — the earlier "Tier 5 not honored" verdict was wrong. The real HARD-band cost is **the
  depth of bespoke knowledge** the agent must (re)discover (Kafka SASL per-producer prefixes — a
  *silent hang*, no failure signal; Fernet 32-byte key math; ioredis URL quirk; libxmlsec/librdkafka
  ABI matching; a SASL-patched Rust fork). Autonomous convergence here needs deep source spelunking
  and many iterations, possibly human-seeded hints on the no-signal discoveries — *that* is the
  honest "best-effort" frontier, not platform capability.
- **INFEASIBLE** — only software that needs **Kubernetes *runtime* orchestration primitives**
  (sidecars-as-pods, init-container semantics, service mesh) inexpressible as
  `prepareCommands`/`initCommands`. Image-only is NOT infeasible (crane-lift). Recon bails here
  before any deploy.

---

## 7. New vs reused — proof the framework + develop flows are untouched

**Reused verbatim:** all of `internal/ops`; `work_session.go` record/load/save + `CloseReasonIterationCap`;
`topology.FailureClass`/`Recovery` + the **34**-signal library (`deploy_failure_signals.go`, not 25)
+ `ops.ClassifyDeployFailure`; `internal/recipe` **emit verbs** (`EmitDeliverableYAML(plan,tierIndex)`
yaml_emitter.go:67, `Tiers/TierAt`, `AssembleRootREADME` assemble.go:64);
`zcp sync recipe create-repo/publish` + `internal/sync/github.go`.

> **Placement corrections (verification §7, two compile-level breaks):**
> 1. **`substituteFragmentMarkers` is unexported** (`assemble.go:619`). Reusing it outside
>    `internal/recipe` needs an export change the plan must list; and all Stage B emit calls must
>    live in **`internal/tools/`** (tools→recipe is permitted; ops/workflow→recipe is depguard-
>    forbidden). `EmitDeliverableYAML` takes a full `*recipe.Plan`, so the **PortSession→Plan
>    conversion** is net-new and unspecified — add it to Phase 4.
> 2. **The FitCeiling builder cannot live in `internal/workflow/`** — its tier-ladder roll-up calls
>    `Tiers()/TierAt()` (in `internal/recipe`), and depguard (`.golangci.yaml:110`) +
>    `architecture_test.go` forbid `workflow`→`recipe`. Fix: **promote the tier ladder to
>    `topology`** (CLAUDE.md promotion rule) or site the builder in the **tools** layer.

**Non-regression proof:** Stage A never calls `HandleEnterPhase/HandleCompletePhase/
BuildScaffoldBrief/BuildFeatureBrief` or any recipe phase gate. Stage B calls only pure
`*Plan→string/file` functions — no phase-engine side effects, no `shape` branch in the phase route.

**New code:** `internal/workflow/port_*.go` (PortSession sidecar `.zcp/state/port/{pid}.json`,
recon classifier, fix-class table, two-counter stall detection, escalation ladder, rubric scorer,
FitCeiling builder); `internal/tools/workflow_port_*.go` (handler boundary; `PhasePortActive` into
the existing build_plan.go fall-through); `internal/recipe` **additive only** (recipe-level surface
contracts + the buildFromGit override).

**The one real emitter change (the only fully-accurate adversarial code finding):**
`writeRuntimeBuildFromGit` (yaml_emitter.go:460-461) hard-codes `RecipeAppRepoBase` + slug +
`-app/-api/-worker`. For an OSS glue repo (`zeropsio/recipe-<slug>`) this emits a **wrong URL** →
additive override: emit `plan.GlueRepo.URL` verbatim when set (port flow only), else fall back to
the hard-coded form (framework path unchanged). Pin with `TestEmitDeliverableYAML_GlueRepoOverride`.

---

## 8. Phased build plan (each shippable + green)

| Phase | Work | Shippable |
|---|---|---|
| **0 Recon + PortSession** | `port_recon.go` (acquisition decision tree on `schema.Cache` + topology: source-build → prebuilt-binary → **crane image-lift**; dep→managed map; band), `port_session.go`; `WorkflowPort`/`PhasePortActive` into build_plan.go fall-through. RED table tests (image-only→crane path, **K8s-runtime-only→bail**, band roll-up). | `workflow=port action=start` classifies a target → band + PortPlan + first candidate yaml, **zero deploy**; bails only on K8s-runtime-orchestration-required software. |
| **1 Deploy-debug loop** | `workflow_port_iterate.go` (derive fix-class from last `FailureClassification`, record), `port_fixclass.go`. Reuses ops + work_session. RED: each derivation, DM-2 guard, scope guard. | EASY (Strapi) runs the full loop agent-driven to C1=C2=C3 passing. |
| **2 Termination + escalation** | `port_progress.go` (two-counter stall, cap, wall budget), `port_escalate.go` (T0/T1/T2). RED: streak survives signal variation, phaseStall on stuck-C3, cap closes, T1 only with prebuilt URL. | Loop self-terminates gracefully; demonstrable on an unwinnable target (bails with partial FitCeiling, never spins). |
| **3 Harden + rubric + FitCeiling** | `port_harden.go` (sentinels, readiness inject, HA probe, tier fitness), `port_rubric.go` (C1–C6 incl. stability hold + C4 checkpoint; roll-up; FitCeiling). | EASY port produces a measured FitCeiling + honest-ceiling report. |
| **4 Stage B capture+publish (CURATED)** | `port_handoff.go`; `workflow_port_capture.go` emits the `environments/<N> — <Name>/import.yaml` output dir from the working deployment, then drives `zcp sync recipe create-repo`+`push-app` (app → `zerops-recipe-apps`) and `publish --software` (envs → `zeropsio/recipes`). `internal/recipe` changes: the D4 recipe-level fragment surfaces (same as framework recipes) + the D6 buildFromGit override pointing at `zerops-recipe-apps/<slug>`. **Fragments (markers) ARE emitted into the repo READMEs; the only thing skipped is the `.zerops-recipe/` FOLDER** (self-service channel). RED: GlueRepoOverride, framework path unchanged. | Full Strapi port → working deploy → app pushed to `zerops-recipe-apps` + recipe published to `zeropsio/recipes` (Strapi entry + fragments, framework-recipe shape). **Complete EASY deliverable.** |
| **5 HARD-band honesty** | cross-service-ordering recon axis + retry-until-ready KB synthesis; UNVERIFIED-constraint surfacing; PostHog canonical HARD fixture. Codex-validate the band classification. | PostHog → honest recipe with **Tier 5 HONORED** (ClickHouse runs HA — mandatory for its ON CLUSTER DDL, per §12); the knowledge-depth residue (Kafka SASL per-producer prefix, etc.) surfaces as `UnresolvedConstraints`, NOT as infeasibility. Honesty contract proven on the hard case. |

---

## 9. Open questions (need confirmation before / during build)

1. **Glue-repo write-authority** — the create-repo/publish path is scriptable + non-interactive,
   but *which org* (`zeropsio` vs `zerops-recipe-apps`) and *which token* has autonomous
   create-permission is an auth/deploy-config decision. If absent, commit is a deferred step and
   `FitCeiling.glueRepo.buildFromGitReady=false` flags it.
2. **Recipe-level marker form — ✅ RESOLVED / VERIFIED (2026-06-09 probe).** Hit the live backend:
   `GET api.app-prg1.zerops.io/api/rest/public/recipe/info?url=github.com/fxck/zrno@main` → HTTP 200,
   `errors: null`, and `extracts` returned **all eight** recipe-level fragments (`name`, `shape`,
   `intro`, `cover`, `description`, `features`, `takeover-guide`, `knowledge-base`) parsed from the
   **paired `#ZEROPS_EXTRACT_START:NAME#`** markers, with `shape: "app"` and all 6 em-dash env tiers.
   The single-marker contingency is dead; zcp's existing `assemble.go` paired-marker machinery is
   reusable as-is — no new marker form. (Stage B still needs `substituteFragmentMarkers` exported +
   the new recipe-level surface contracts; only the *marker syntax* question is closed.)
3. **Cross-service runtime readiness** — is there *any* Zerops primitive beyond `priority` +
   per-service `readinessCheck` to barrier B's start on A being healthy? Current evidence: no. If
   so, HARD band is usable only for OSS that tolerates eventual-consistency startup — quantify how
   many real targets qualify (is HARD a usable band or a near-empty set?).
4. **Self-run ClickHouse/Kafka production HA** — managed catalog advertises HA-mode, but a
   self-coordinated analytics pipeline reaching production HA is unverified. Must HARD always cap
   C6 at 1 until a real HA integration test lands?
5. **PortSession compaction recovery + EngineVersion stamping** — does the port loop need the same
   `action=status` envelope recovery as develop/recipe, and version-stamping to refuse a
   stale-binary mid-port resume? **Verification §5: yes, and it's net-new code, not reuse** —
   `PhasePortActive` in the fall-through means `action=status` returns an empty Plan, so port needs
   its own `port_status_recovery.go` (launch-production needed a dedicated `launch_status_recovery.go`
   for exactly this, P4). And `Plan.EngineVersion`/`gate_engine_version_stamped.go` is a
   recipe-engine mechanism; `WorkSession.Version` is only a schema version — port builds a new
   stamping check, it does not reuse one.
6. **C4 probe authorship contract** — the minimal structured shape the handler must specify
   (endpoint hint / expected-status / browser assertion) so the agent's probe is comparable across
   iterations and the rubric grades deterministically, not by re-judging prose.

---

## 10. Honest verdict

For **EASY-band** OSS (managed deps + standard runtime — Strapi, Ghost, most CMS): **fully
autonomous porting is realistic** and produces a complete `shape:software` recipe. For **HARD-band**
(PostHog-class): the platform **can host it end-to-end including HA** — `fxck/recipe-posthog` proves
it (crane-lifted image, 9 runtimes, ClickHouse HA, full event pipeline). The constraint is not
feasibility; it's the **depth of OSS-internals + platform-quirk knowledge** an autonomous agent must
rediscover, some from symptoms that emit no failure signal (a SASL producer hanging forever). The
agent-driven loop is the right vehicle — the LLM reads source and authors glue while the handler
scaffolds — but HARD-band convergence is iteration-heavy and may need human-seeded hints on the
no-signal discoveries; "best-effort" is honest about that. The only true bail is software that needs
**Kubernetes *runtime* orchestration primitives** that can't be expressed as
`prepareCommands`/`initCommands`. **Image-only is in-band via crane-lift.**

---

## 11. Verification amendments (independent verification, 2026-06-09)

Cross-ref `plans/oss-recipe-port-flow-verification-2026-06-09.md`. The verification confirmed the
architecture story and every load-bearing platform claim, and surfaced corrections — all folded in
above except these residual ones, tracked here:

- **D6 is half-kept (must cover TWO emit sites).** §7's "one real emitter change" fixes only
  `writeRuntimeBuildFromGit` (the runtime site). The software-shape plan's D6 also requires the
  `ServiceKindUtility` branch (`yaml_emitter.go:413-415`, today emits only `zeropsSetup`) to gain a
  `buildFromGit` emit path — and for a `shape:software` recipe whose primary service IS the
  glue/utility (the mailpit micro-model), the **utility site is the more important of the two**.
  Phase 4 must change both.
- **B2 / OQ-1 glue-repo org is bigger than stated.** `zcp sync recipe create-repo` is config-pinned
  to `zerops-recipe-apps/{slug}[-suffix]` via `.sync.yaml` (`org: zerops-recipe-apps`) — **not**
  the `zeropsio/recipe-<slug>` form §2/§7 imply. It's a CLI subcommand on ambient `gh auth` (not an
  MCP tool), so the autonomous agent reaches it only via shell, and *which token holds org-create
  rights* is unresolved. Reconcile the naming and the auth path in Phase 4/OQ-1.
- **The C-check → tier mapping is an invented semantic layer — state it as such.** `tiers.go` is a
  *deployment-topology* ladder (AI Agent → … → HA Prod); nothing in "Tier 4" intrinsically means
  "data persists across redeploy." The §5 rubric→tier projection is a new mapping the port flow
  introduces; it is workable but should be documented as a port-flow invention, not an existing
  `tiers.go` property.
- **A2 subdomain works by accident — make the ServiceMeta decision explicit in Phase 0.**
  `zerops_import` writes no `ServiceMeta`, and `serviceEligibleForSubdomain` is permissive for
  meta-less services (`deploy_subdomain_test.go:128`), so raw-import + standard deploy auto-enables
  the subdomain. But the plan never decides whether port writes `ServiceMeta`s — if it does, with
  any non-allowlisted `Mode`, `modeAllowsSubdomain` defaults false and auto-enable **silently
  skips**. Phase 0 must decide this.
- **`services[].mode` is deprecated** in the live import schema ("use Type version only"), though
  ZCP's export/launch composers still emit it (`bundle/export.go:133`, `bundle/launch.go:193`). The
  emitted import.yamls inherit a deprecated field — acceptable (parity with existing composers) but
  noted.

**Net effect on the phased plan:** Phase 0 gains the ServiceMeta/subdomain decision; Phase 1 gains
the persisted-`Signals` field + the import-override budgeting; Phase 3's FitCeiling builder needs
the tier-ladder promoted to `topology` (or sited in tools); Phase 4 gains the `substituteFragmentMarkers`
export, the PortSession→Plan conversion, the D6 *second* emit site, and the org/auth reconciliation;
the status-recovery + version-stamping is net-new code, not reuse. **The feasibility thesis is
unchanged** — these are build-correctness amendments, not a redesign.

---

## 12. Reality check: the real PostHog port (`fxck/recipe-posthog`) — feasibility CORRECTIONS

A working, hand-built PostHog port (`github.com/fxck/recipe-posthog`, the glue/`buildFromGit` repo)
**refutes the §6/§10 pessimism** the design workflow produced. Corrections, all evidenced by the
repo's `zerops-import.yaml` + `zerops.yml` + `utils/`:

1. **Image-only is NOT a bail — `crane`-lift is the third acquisition path.** `utils/init.sh` runs
   `crane export posthog/posthog:latest` (+ `posthog-node`), extracts the image rootfs
   (`code`, `python-runtime`, `docker-entrypoint.d` + ABI-matched `libxmlsec1`/`librdkafka` shared
   libs) into `/opt/posthog` on a stock `ubuntu/python@3.12` / `ubuntu/nodejs@24` runtime, repoints
   the venv at Zerops's Python, symlinks PostHog's hardcoded `/code`, and swaps Nginx Unit→gunicorn.
   **You never needed a native image-deploy schema field.** Acquisition ladder = source-build →
   prebuilt-binary → **crane image-lift**. Recon must add this branch; the §3-A0 "image-only →
   BAIL" rule is deleted.
2. **All deps managed; ClickHouse runs HA and it's mandatory.** `clickhouse@25.3 mode: HA` because
   PostHog's `ON CLUSTER` DDL targets the `zerops` cluster that exists only in HA mode (+ Keeper +
   macros). Kafka@3.9, Valkey@7.2, Postgres@18 (×2 — a dedicated `cyclotrondb`), object-storage,
   Mailpit. **"Tier 5 HA not honored" was wrong — PostHog *requires* HA ClickHouse to boot at all.**
3. **Cross-service ordering is solved, not a ceiling.** `zsc execOnce <key> --retryUntilSuccessful`
   sequences `ch-init` → `migrate` → `migrate_clickhouse` → cyclotron migrations across services;
   `zsc scale ram max 30m` pre-boosts RAM so the Django/Celery startup spike doesn't OOM before the
   autoscaler reacts. The "no native readiness barrier" gap is real but a standard, repeatable
   workaround — downgrade it from near-fatal to a known idiom.
4. **The real HARD-band frontier is KNOWLEDGE DEPTH, not feasibility.** This port encodes
   discoveries a deterministic fix-class loop cannot derive, several with **no failure signal at
   all**: Kafka SASL needs a *separate prefix per producer mode* (`PRODUCER`/`CONSUMER`/`CDP`/
   `METRICS`/`WARPSTREAM`/`WAREHOUSE`) or a producer hangs forever on idempotence-PID acquisition,
   silently blocking `listen()`; `ENCRYPTION_SALT_KEYS` must be exactly 32 ASCII chars (Fernet
   base64 math); ioredis treats a bare hostname as localhost so the full `redis://` URL is required;
   `CDP_API_URL`'s default is an unresolvable k8s DNS name; `capture` needed a **SASL-patched Rust
   fork** (`fxck/posthog-capture`). **Implication for the autonomous loop:** the agent-driven
   architecture is *vindicated* (only an LLM reading PostHog's source + reasoning about a silent
   hang could find these), but HARD-band convergence is iteration-heavy and the no-signal class
   (C2 "boots-stable" catches the *symptom*; the *fix* needs source knowledge) is where best-effort
   may legitimately need human-seeded hints or a curated per-OSS knowledge pack. This belongs in the
   FitCeiling's `unresolvedConstraints` honesty, and argues for an **OSS-knowledge-seeding input** to
   recon (Phase 0/5) so the loop starts from known gotchas rather than rediscovering silent hangs.

5. **This repo is a Stage-A artifact, not a finished recipe.** It's the glue/`buildFromGit` repo
   (one `zerops-import.yaml`, `zerops.yml`, `utils/`) — **no `.zerops-recipe/` folder**, because
   that folder is the self-service GitHub channel, NOT the curated path. `recipe-posthog` is exactly
   an **app/glue repo** of the kind that belongs in `zerops-recipe-apps`. Stage B (not yet applied)
   would push it there and `publish` the env import.yamls to the `zeropsio/recipes` catalog.

6. **CHANNEL CORRECTION — a curated OSS recipe is shaped EXACTLY like a framework recipe.** Curated
   path: **app/glue → `zerops-recipe-apps/<slug>`**, **recipe envs + metadata → `zeropsio/recipes`**
   via `zcp sync recipe create-repo`/`push-app`/`publish` (`.sync.yaml` pins both). The **Strapi
   catalog entry is the same fields as a framework recipe** (name/software/categories/cover/env
   structure). **The rest is FRAGMENTS** — `description`/`features`/`takeover-guide`/
   `knowledge-base` + per-service `integration-guide` authored as `#ZEROPS_EXTRACT_START:NAME#`
   markers in the **repo READMEs** (app repo + per-service buildFromGit repos), the **same machinery
   framework recipes use** — so the D4 fragment surfaces + `substituteFragmentMarkers` ARE on the
   path (an earlier draft wrongly dropped them). The **only** self-service-specific artifact the
   port flow does NOT produce is the **`.zerops-recipe/` FOLDER layout** (a user's own public repo
   the frontend live-fetches via `/api/recipe/info` — what `zrno-shop` is). So the OQ-2 marker probe
   IS relevant: curated repo READMEs use the same paired markers. **Net: Stage B = the existing
   framework-recipe capture/publish, reused wholesale, fed by the ported deployment.**

**Bottom line:** PostHog is the existence proof that the platform side has *no* hard ceiling short of
K8s-runtime-only software. The honest limit on *autonomous* porting is the agent's ability to
rediscover deep, sometimes signal-less OSS internals — which reshapes the design toward
knowledge-seeding + iteration budget, and away from "declare it infeasible."
