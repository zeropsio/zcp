# v39 Commit 1 Day 1 — bullet audit of recipe_templates.go prose functions

**Scope**: classify every hardcoded bullet in the 4 prose-generating functions
in [`internal/workflow/recipe_templates.go`](../../../internal/workflow/recipe_templates.go)
so Day 2's refactor has a decision per bullet. Per [`v39-fix-stack.md`](v39-fix-stack.md) §Commit 1:

> For (b) bullets: user decides per-bullet — promote to schema + add
> yaml-generator support, or drop the bullet. Skipping this audit causes
> either silent teaching loss or fabrication migration into schema.

**Classifications**:
- **(a) plan-backed** — bullet claim corresponds to a field the yaml generator
  currently emits. Safe to rewrite as a deterministic compose from plan/yaml.
- **(b) prose-only** — bullet claims something no yaml field and no
  `RecipePlan` field backs. Must be dropped, reworded, or promoted to schema
  (with yaml-generator support) before the refactor ships.
- **(c) system-invariant** — bullet is true of every recipe by construction
  (audience positioning, platform design, tier naming). Safe to keep verbatim
  in a template list.

**What I checked**:
- Read [`recipe_templates.go`](../../../internal/workflow/recipe_templates.go) L220-403
  (the four prose functions).
- Read [`recipe_templates_import.go`](../../../internal/workflow/recipe_templates_import.go)
  (the yaml generator) — enumerated every field `writeSingleService` /
  `writeDevService` / `writeStageService` / `writeAutoscaling` /
  `writeProjectSection` emit per tier.
- Read [`recipe.go`](../../../internal/workflow/recipe.go) — confirmed
  `RecipePlan` / `RecipeTarget` / `ResearchData` schema. No
  `BackupPolicy`, `HealthCheck`, `ReadinessCheck`, `DevContainerImage`,
  `DevToolchain` fields exist.

**Yaml fields actually emitted per tier** (source-of-truth for the three-way-equality test):

| Field | Where | Tiers |
|---|---|---|
| `project.name` | every | every tier distinct via suffix |
| `project.corePackage: SERIOUS` | env 5 only | env 5 |
| `project.envVariables.{APP_SECRET}` | if `plan.Research.NeedsAppSecret` | every (same) |
| `project.envVariables.{per-env}` | `plan.ProjectEnvVariables[envIndex]` | varies |
| dev service (hostname + zeropsSetup: dev + minRam 1) | `writeDevService` | env 0-1 runtime only |
| stage service (`{host}stage` + prod zeropsSetup) | `writeStageService` | env 0-1 runtime only |
| single service (`{host}` + prod zeropsSetup) | `writeSingleService` | env 2-5 runtime + all non-runtime |
| `mode: HA` | env 5 + ServiceSupportsMode | env 5 only |
| `mode: NON_HA` | env 0-4 + ServiceSupportsMode | env 0-4 |
| `minContainers: 2` | env >= 4 runtime | env 4+ |
| `objectStorageSize: 1 / policy: private` | object storage | every |
| `verticalAutoscaling.minRam` | every with ServiceSupportsAutoscaling | see below |
| `verticalAutoscaling.cpuMode: DEDICATED` | env 5 non-utility | env 5 only |
| `enableSubdomainAccess: true` | runtime non-worker + utility | every |
| `priority: 5` / `priority: 10` | API / non-runtime | every |

**Autoscaling tier table** (from `writeAutoscaling` L327-372):

| Tier | Runtime minRam | Non-runtime minRam | minFreeRamGB | Extra |
|---|---|---|---|---|
| 0 | 0.5 | 0.25 | — | — |
| 1 | 0.5 | 0.25 | — | — |
| 2 | 0.5 | 0.25 | — | — |
| 3 | 0.5 | 0.25 | 0.25 | — |
| 4 | 0.5 | 0.25 | 0.25 | — |
| 5 utility | — | 0.25 | — | — |
| 5 runtime | 0.5 | — | 0.25 | DEDICATED |
| 5 managed | — | 1.0 | 0.5 | DEDICATED |

**Key finding: env 3 and env 4 emit IDENTICAL autoscaling output.** Every
bullet claiming "managed services scale up at env 4" or "stage is lower-scale
than small-prod" is (b) prose-only — the current code doesn't back it.

---

## 1. envAudience(envIndex) — L220-262

Tier 0 (AI Agent):
- All 5 bullets = **(c)** — audience positioning. Keep.

Tier 1 (Remote CDE):
- Bullet 2: *"Iteration happens inside a Zerops dev container with the full
  toolchain preinstalled (framework CLI, compiler, lint, debugger)."* →
  **(b)** — **Cluster A**. Env 0 and env 1 emit identical dev-slot yaml.
  No field differentiates a "CDE toolchain" from an "agent toolchain".
  Same claim as editorial-review CRIT #1 on v38.
- Other 4 bullets = (c). Keep.

Tier 2 (Local):
- All 5 bullets = (c). Keep.

Tier 3 (Stage):
- Bullet 2: *"Stage runs the same `setup: prod` zerops.yaml as every higher
  tier."* → **(a)**. `recipeSetupName(target, false)` is prod for every env
  2-5 runtime. Plan-backed.
- Bullet 3: *"Scale is intentionally lower (`minContainers: 1`, single-replica
  data services)."* → **(a)** for single-replica (mode: NON_HA at env 3);
  **(b)** for literal `minContainers: 1` — yaml omits the field entirely at
  env 3. **Cluster F** (reword as "single-replica by default" without
  quoting `minContainers: 1`).
- Bullet 4: *"HealthCheck timing is relaxed so flaky-first-start deploys
  still land; production tiers tighten it."* → **(b)** — **Cluster B**.
  No `healthCheck` / `readinessCheck` field is emitted by the env
  import.yaml generator. healthCheck lives in per-codebase zerops.yaml
  (agent-authored), not env import.yaml.
- Bullet 5 = (c). Keep.

Tier 4 (Small Production):
- Bullet 1 (three sub-bullets) = (c) audience. Keep.
- Bullet 2: *"Runtime services run at `minContainers: 2` (scaled vertically
  for cost); DB and cache are single-replica and run in `mode: NON_HA`."* →
  **(a)**. minContainers: 2 at env >= 4, mode: NON_HA at env < 5.
- Bullet 3: *"Daily backups are retained per the recipe's backup policy."*
  → **(b)** — **Cluster C**. No backup field in yaml, no `BackupPolicy`
  in RecipePlan. Platform admin concern, not recipe yaml.
- Bullet 4: *"Rolling deploys are graceful when `readinessCheck` is
  configured — one replica keeps serving while the other rolls. DB/cache
  remain NON_HA, so node-level failures incur downtime."* → second half
  **(a)** (NON_HA); first half **(b)** — **Cluster B** (no readinessCheck
  emitted).
- Bullet 5 = (c). Keep.

Tier 5 (HA Production):
- Bullet 1 = (c). Keep.
- Bullet 2: *"Runtime runs `minContainers: 2` behind the L7 balancer."* →
  **(a)** (minContainers: 2 at env 5). "L7 balancer" is (c) platform-level.
- Bullet 3: *"DB and cache are in replicated HA mode."* → **(a)**
  (mode: HA at env 5 for ServiceSupportsMode services).
- Bullet 4: *"Object storage and search engine carry the redundancy their
  managed types provide."* → **(c)** platform-invariant (managed types
  don't change across tiers; redundancy is per type). Keep, unchanged.
- Bullet 5 = (c). Keep.

---

## 2. envDiffFromPrevious(envIndex) — L269-309

Case 1 (env 1 vs env 0):
- Bullet 1: *"Runtime containers carry an expanded toolchain — IDE Remote
  server, shell customizations, language-specific debug tools."* → **(b)** —
  **Cluster A**. **This is editorial-review CRIT #1 verbatim.**
- Bullet 2: *"Managed services are unchanged — same single-replica DB, cache,
  broker, storage, search."* → **(a)**. Env 0 and env 1 emit identical
  managed-service blocks (same mode, same autoscaling).
- Bullet 3: *"Every `zerops.yaml` setup name is identical; only the dev
  container image differs."* → first half **(a)** (setup name same);
  second half **(b)** — **Cluster A** ("dev container image differs" has
  no backing field).
- Bullet 4 = (c). Keep.

Case 2 (env 2 vs env 1):
- Bullet 1: *"Runtime dev services are not deployed — the dev container is
  optional at Local."* → **(a)**. Env 2 uses `writeSingleService` (no
  `writeDevService` call for env >= 2).
- Bullets 2, 3, 4 = (c). Keep.
- Bullet 5: *"Stage runtime (if deployed) remains unchanged from lower tiers."*
  → **(a) with reword**. Env 1 emits `{host}stage` via writeStageService;
  env 2 emits `{host}` via writeSingleService. Autoscaling + zeropsSetup
  are same, but the **hostname changes** (`apistage` → `api`). The bullet
  is technically misleading. **Rewording recommendation**: "The stage
  runtime collapses to a single-slot `{host}` service at Local — same
  `setup: prod` build, same autoscaling, but the `{host}stage` pair-with-dev
  shape from env 0-1 is gone." Plan-backed after reword.

Case 3 (env 3 vs env 2):
- Bullet 1: *"Dev runtime services disappear entirely — Stage is
  deployment-only, no iteration container."* → **(a)**. No dev services
  emitted at env 3.
- Bullet 2: *"Stage runtime containers are deployed from the `setup: prod`
  block."* → **(a)** (zeropsSetup from recipeSetupName).
- Bullet 3: *"Every runtime service runs at `minContainers: 1`."* → **(b)**
  — **Cluster F**. No `minContainers` emitted at env 3.
- Bullet 4: *"Managed services stay at their tier-3 scale (single-replica)."*
  → **(a)** (mode: NON_HA, same autoscaling).
- Bullet 5: *"HealthCheck uses relaxed readiness thresholds."* → **(b)** —
  **Cluster B**.

Case 4 (env 4 vs env 3):
- Bullet 1: *"Managed services scale up where throughput matters — DB
  container size grows, cache memory allocation grows, search engine index
  capacity grows."* → **(b)** — **Cluster D**. **Env 3 and env 4 autoscaling
  is IDENTICAL in current code.** Either promote to schema (actually scale
  up at env 4) or drop the claim.
- Bullet 2: *"Runtime services run at `minContainers: 2` — the second replica
  provides redundancy during rolling deploys."* → **(a)** (minContainers: 2
  at env >= 4).
- Bullet 3: *"DB and cache remain single-replica (`mode: NON_HA`)."* →
  **(a)**.
- Bullet 4: *"Backups become meaningful at this tier — daily snapshots of
  DB and storage are retained."* → **(b)** — **Cluster C**.
- Bullet 5: *"Same `zerops.yaml`, same setup name — the tier difference is
  all in `import.yaml` sizing."* → **(a) with reword**. The literal yaml
  difference between env 3 and env 4 is ONLY `minContainers: 2` on runtime
  services. "Sizing" overclaims; **reword to**: "Same `zerops.yaml`, same
  setup name — the only tier difference is `minContainers: 2` on runtime
  services."

Case 5 (env 5 vs env 4):
- Bullet 1: *"`minContainers: 2` is already in place at Small Production —
  the HA-distinct change is `mode: HA` on DB and cache (automatic failover
  on node failure) and tighter readiness-probe windows."* → first parts
  **(a)**; "tighter readiness-probe windows" **(b)** — **Cluster B**.
- Bullets 2, 3: `mode: HA` → **(a)**.
- Bullet 4: *"Object storage gets redundancy built into its managed type."*
  → **(c)**. Keep.
- Bullet 5: *"Workers add a queue-group so the second replica does NOT
  double-process jobs."* → **(c)** platform-teaching. Keep.
- Bullet 6: *"Health checks tighten — readiness probes now gate traffic
  handoff; SIGTERM drain windows are honored."* → **(b)** — **Cluster B**
  (readiness-probe half); SIGTERM half (c). Split and drop readiness claim.
- Bullet 7: *"Same application code — the tier difference is replica counts
  and service modes."* → **(a)**. At env 4→env 5: `minContainers: 2` stays
  the same; env 5 adds `cpuMode: DEDICATED`, `mode: HA`, managed-service
  minRam bumps for non-utility non-runtime (0.25 → 1.0). Plan-backed after
  reword to include DEDICATED + minRam bump.

---

## 3. envPromotionPath(envIndex) — L314-352

Case 0 (env 0 → env 1):
- Bullet 1 = (c). Keep.
- Bullet 2: *"The dev container in the new project ships the CDE toolchain;
  the managed-service set stays the same shape."* → first half **(b)** —
  **Cluster A**; second half **(a)**.
- Bullet 3: *"Each tier's YAML declares a distinct `project.name` suffix
  (this tier's ends `-agent`, Remote's ends `-remote`)."* → **(a)** —
  `project.name` suffix is per-tier from `envTiers[i].Suffix`.
- Bullet 4: *"Handoff cost: minutes — no code changes, no deploy ceremony."*
  → **(c)** operational framing. Keep.

Case 1 (env 1 → env 2):
- All 5 bullets = (c). Keep.

Case 2 (env 2 → env 3):
- Bullet 1 = (c).
- Bullet 2: *"Stage runs the same `setup: prod` zerops.yaml that every
  higher tier uses."* → **(a)**.
- Bullet 3: *"Stage gives you a live subdomain."* → **(a)**
  (enableSubdomainAccess: true for non-worker runtime).
- Bullet 4 = (c).

Case 3 (env 3 → env 4):
- Bullet 1 = (c).
- Bullet 2: *"`zerops.yaml` itself does not change — same prod setup, same
  build + deploy commands."* → **(a)**.
- Bullet 3: *"Managed-service sizing grows: larger DB, more cache memory,
  bigger search index."* → **(b)** — **Cluster D**. False.
- Bullet 4: *"Runtime services run at `minContainers: 2`."* → **(a)**.
- Bullet 5: *"First tier where the recipe's backup policy matters."* →
  **(b)** — **Cluster C**.
- Bullet 6: *"Configure `deploy.readinessCheck` on runtime services before
  this promotion."* → **(c) platform teaching**. Keep (it's advice to the
  reader to add the field in their codebase's zerops.yaml, not a claim
  about the env import.yaml).

Case 4 (env 4 → env 5):
- Bullet 1 = (c).
- Bullet 2: *"Runtime services continue at `minContainers: 2`; the
  HA-distinct change is `mode: HA` on DB and cache. Note: managed-service
  `mode` is immutable after creation."* → **(a)**.
- Bullet 3, 4, 5, 6 = (c). Keep.

---

## 4. envOperationalConcerns(envIndex) — L357-403

Tier 0:
- Bullets 1, 3, 4, 5 = (c). Keep.
- Bullet 2: *"`zerops.yaml` `setup: dev` ships a no-op start (`zsc noop
  --silent`)."* → **(c) with caveat**. `zsc noop --silent` is authored by
  the writer in per-codebase zerops.yaml, not emitted by the env import.yaml
  generator. Platform-invariant for dev slots. Keep but flag in docstring.

Tier 1:
- All 5 bullets = (c). Keep.

Tier 2:
- Bullets 1, 3, 4, 5 = (c). Keep.
- Bullet 2: *"Service hostnames (`db`, `cache`, `queue`, `storage`,
  `search`) resolve through the VPN."* → **(b)** — **Cluster G**.
  The hardcoded list is not guaranteed to match `plan.Targets[].Hostname`
  (e.g. a plan may name its DB `pg` or its cache `valkey`). Reword to:
  "Each managed service hostname in the plan resolves through the VPN."
  Plan-backed after reword (iterate plan.Targets and interpolate the
  actual hostnames).

Tier 3:
- Bullet 1: *"Stage deploys go through the `cross-deploy` path — dev
  container's committed code is pushed to the stage service."* → **(b)**.
  At env 3 there is no dev container in Zerops — dev was env 0-1 only.
  "cross-deploy from dev to stage" applies at env 0-1, not env 3.
  The reader at env 3 pushes from their local machine or CI. Reword or
  drop.
- Bullet 2: *"Stage has no long-lived dev process; every deploy produces
  a fresh container."* → **(a)** (no writeDevService at env 3).
- Bullet 3: *"HealthCheck timing is relaxed at this tier — slow
  first-start deploys still land."* → **(b)** — **Cluster B**.
- Bullet 4: *"If stage deploys are flaky, check healthCheck windows in
  zerops.yaml."* → **(c) platform teaching**. This is advice about the
  codebase's zerops.yaml (which is writer-authored), not a claim about
  env import.yaml. Keep.
- Bullet 5: *"Stage hits the same DB as dev on tiers 0-2 — schema
  migrations break backwards compatibility at your own risk."* → **(b)**
  — **Cluster E**. **Editorial-review CRIT #3 verbatim.** Every tier
  declares a distinct `project.name`; stage has its own project with its
  own DB. Drop entirely.
- Bullet 6 = (c). Keep.

Tier 4:
- Bullets 1, 2, 4, 5 = (c) platform teaching. Keep.
- Bullet 3: *"Backups matter: verify the daily DB snapshot policy is
  active."* → **(b)** — **Cluster C**.
- Bullet 6: *"Cache is single-replica (`mode: NON_HA`) — a cache restart
  flushes warm data."* → **(a)** (mode: NON_HA at env 4).

Tier 5:
- All 6 bullets = (c). Keep.

---

## 5. Clusters summary — user decisions needed

Six (b)-rooted clusters collapse the ~25 individual (b) bullets into 5-6
structural decisions. Each needs a drop / reword / promote-to-schema call.

### Cluster A — env 0 vs env 1 "toolchain" differentiation

Claims: "CDE toolchain preinstalled", "runtime containers carry an expanded
toolchain", "dev container image differs", "dev container ships the CDE
toolchain".

Backing data: NONE. `writeDevService(target)` emits an identical yaml block
for env 0 and env 1 (same `zeropsSetup: dev`, same minRam, same autoscaling).
The "expanded toolchain" / "different image" is a conceptual distinction
the image system does not expose at the import.yaml level.

Affected bullets: envAudience(1).2; envDiffFromPrevious(1).1, (1).3-second-half;
envPromotionPath(0).2-first-half.

**Options**:
- **(A1) Drop all toolchain/image claims.** Keep the audience framing
  (human developer via CDE vs AI agent) without claiming container
  differences. Recommended — the distinction IS audience, not container-tech.
  The dev slot runs the same image; how you connect to it differs (IDE
  Remote vs SSH-plus-zcp).
- **(A2) Promote to schema.** Add `CDEImage string` or `DevContainerExtras
  []string` to `RecipeTarget` or to `envTiers`; extend writeDevService to
  emit an additional field at env 1. But Zerops doesn't actually have a
  "CDE image" layer separate from dev image — this may not be a real thing
  to promote.

### Cluster B — healthCheck / readinessCheck tier-tuning

Claims: "HealthCheck timing is relaxed at tier 3", "production tiers
tighten it", "readinessCheck is configured" (implied by env 4/5 prose),
"tighter readiness-probe windows at env 5".

Backing data: NONE at the env import.yaml level. `readinessCheck` is a
zerops.yaml field, authored by the writer sub-agent per-codebase, not
emitted by GenerateEnvImportYAML. No tier-specific tuning exists in code.

Affected bullets: envAudience(3).4; envAudience(4).4-first-half;
envDiffFromPrevious(3).5; envDiffFromPrevious(5).1-last-clause;
envDiffFromPrevious(5).6-first-half; envOperationalConcerns(3).3.

**Options**:
- **(B1) Drop all tier-specific healthCheck claims.** Recommended.
  healthCheck lives in codebase zerops.yaml, not env import.yaml — it's
  out of scope for env README prose. Platform-teaching advisories
  ("configure readinessCheck on runtime services") are fine; specific
  claims that the tier's yaml TUNES it are false.
- **(B2) Promote to schema.** Add tier-specific `readinessCheck` to env
  import.yaml (but readinessCheck isn't an import.yaml field — it's a
  zerops.yaml field, so this would be a schema extension on a different
  file). Not recommended.

### Cluster C — backup-policy claims

Claims: "Daily backups are retained per the recipe's backup policy",
"Backups become meaningful at this tier", "verify the daily DB snapshot
policy is active", "First tier where the recipe's backup policy matters".

Backing data: NONE. No `BackupPolicy` field in `RecipePlan`; no backup
field in yaml generator output. Zerops platform backup config is admin-
only (via dashboard, not import.yaml).

Affected bullets: envAudience(4).3; envDiffFromPrevious(4).4;
envPromotionPath(3).5; envOperationalConcerns(4).3.

**Options**:
- **(C1) Drop all backup claims.** Recommended. Not a recipe-yaml concern;
  handled at platform admin level.
- **(C2) Promote to schema.** Add `BackupPolicy` to RecipePlan and emit
  some form of backup stanza. But Zerops import.yaml doesn't define a
  backup stanza — this would be teaching the reader to configure it in
  the dashboard, which isn't an import.yaml claim. Not recommended.

### Cluster D — env 3 → env 4 managed-service "scales up" claim

Claims: "Managed services scale up where throughput matters — DB container
size grows, cache memory allocation grows, search engine index capacity
grows", "Managed-service sizing grows: larger DB, more cache memory,
bigger search index", "the tier difference is all in `import.yaml` sizing".

Backing data: FALSE. `writeAutoscaling` emits IDENTICAL autoscaling for
env 3 and env 4. The only yaml delta at env 4 is `minContainers: 2` on
runtime services.

Affected bullets: envDiffFromPrevious(4).1; envDiffFromPrevious(4).5-last-half;
envPromotionPath(3).3.

**Options**:
- **(D1) Drop the scaling-grows claim.** Reword all three bullets to
  reflect "only `minContainers: 2` on runtime services differs from env 3"
  — the actual yaml delta. Consistent with current code.
- **(D2) Promote to schema.** Extend `writeAutoscaling` to bump
  managed-service minRam at env 4 (e.g. 0.25 → 0.5, add minFreeRamGB).
  Makes the prose true. Requires a platform-team conversation about
  whether env 4 non-runtime SHOULD scale up vs env 3 — the fact that
  env 3 and env 4 are identical today suggests the design intent was
  "only HA at env 5 matters; intermediate tiers share sizing." If design
  intent is preserved, pick (D1).

**Recommended**: (D1). Env 3 / env 4 sharing managed-service sizing
(with only minContainers differing) is a defensible design. The
"scale up" claim was aspirational prose.

### Cluster E — "Stage hits the same DB as dev on tiers 0-2"

Claim: single bullet in `envOperationalConcerns(3)`, editorial-review CRIT #3.

Backing data: FALSE. Every tier emits `project.name: {slug}-{suffix}`
distinct. Each tier is a separate Zerops project with its own DB.

**Options**: DROP ONLY. There's no schema promotion that makes this true
without changing the platform semantics.

### Cluster F — literal `minContainers: 1` at env 3

Claim: "Every runtime service runs at `minContainers: 1`".

Backing data: env 3 does not emit a `minContainers` line at all. Zerops
platform default is 1, but the yaml doesn't quote it.

**Options**:
- **(F1) Reword**: "Runtime services run single-replica (minContainers
  defaults to 1)" or "No explicit `minContainers` — platform default of
  1 applies". Recommended.
- **(F2) Promote to schema**: explicitly emit `minContainers: 1` at env 3.
  Trivial change (one line in writeSingleService), makes the prose
  literally true. Platform behavior unchanged.

### Cluster G — hardcoded service-hostname list at env 2

Claim: "Service hostnames (`db`, `cache`, `queue`, `storage`, `search`)
resolve through the VPN."

Backing data: `plan.Targets[].Hostname` — actual hostnames come from the
plan. The hardcoded list may or may not match.

**Options**:
- **(G1) Derive from plan**: iterate `plan.Targets` non-runtime entries
  and interpolate actual hostnames. Recommended.
- **(G2) Drop the specific list**: "Your plan's managed-service hostnames
  resolve through the VPN." Generic-but-correct fallback.

---

## 6. Recommended defaults (user can override any)

| Cluster | Recommendation | Why |
|---|---|---|
| A — toolchain | **Drop.** Reframe tier 0→1 as audience-only. | No image-level distinction exists; claim was aspirational. Zerops doesn't expose tier-specific dev images. |
| B — healthCheck | **Drop tier-specific claims; keep platform-teaching advisories.** | healthCheck is zerops.yaml (writer-authored), not env import.yaml. Cross-surface concern. |
| C — backups | **Drop.** | Admin-level platform config, not recipe yaml. |
| D — env 3→4 scaling | **Drop.** Reword to "only `minContainers: 2` differs". | Matches current code. Design intent appears to be "HA at env 5 matters, intermediate tiers share sizing". |
| E — stage DB | **Drop.** | Factually wrong. No schema change can make it true. |
| F — minContainers: 1 | **Reword.** "Single-replica (platform default)". | Cheaper than promote. |
| G — hostname list | **Promote to derived-from-plan.** | Small change, matches actual plan shape. |

If the user accepts this defaults matrix, the Day-2 refactor proceeds with
all (b) bullets either dropped or reworded and zero new plan fields. If
the user picks (A2) / (B2) / (C2) / (D2) for any cluster, Day 2 also
touches `recipe.go` (schema extensions) and `recipe_templates_import.go`
(yaml emission).

---

## 7. What Day 2 builds with this audit

After user decision:

- **`EnvTemplate` struct** (new, in recipe.go or recipe_templates.go) —
  composes from plan + system-invariant templates. Fields for: audience
  (from envTiers metadata), diff-from-adjacent (computed against prev-tier
  yaml fields), promotion-path (computed against next-tier yaml fields),
  operational-concerns (system-invariant + cross-surface citations).
- **Rewritten prose functions** take `plan *RecipePlan` + `envIndex`.
  No function returns a hardcoded switch-case string; every claim routes
  through either a plan-read or a system-invariant template list.
- **Gold test** `TestFinalizeOutput_PassesSurfaceContractTests`
  (Day 3): for every bullet in the rendered env README, either (a) the
  bullet is whitelisted as (c) system-invariant from a known template-list
  constant, OR (b) the bullet cites a specific plan field / yaml-generator
  output and the three-way equality (`plan.X` == `emitYaml(plan)[X]` ==
  `prose.cites(X)`) holds. Any divergence fails with file:line pointer
  at which layer broke.

---

## 8. User decision — accepted defaults (2026-04-22)

User direction: *"do what will produce the best the most precise and
most consistent output."*

All §6 defaults accepted:
- Clusters A, B, C, D, E: **drop** — promotion-to-schema would add
  fabrication at layers the yaml generator doesn't reach (A no platform
  dev-image concept; B healthCheck is codebase zerops.yaml not env
  import.yaml; C backups are platform-admin-only; D redesigning tier
  scaling is out of Commit 1 scope; E is factually wrong with no
  promotion path).
- Cluster F: **reword** to "single-replica (platform default)" —
  respects Zerops yaml convention of omitting defaults.
- Cluster G: **derive from `plan.Targets`** — cheapest precise option
  since Commit 1's refactor already passes `plan` into every prose
  function.

Day 2 scope locked: zero new fields on `RecipePlan` / `RecipeTarget` /
`ResearchData`; zero changes to `recipe_templates_import.go` (yaml
generator stays as-is); changes confined to `recipe_templates.go` (prose
functions) + `recipe_templates_test.go` (existing tests) + new gold test
`recipe_templates_surface_test.go`.
