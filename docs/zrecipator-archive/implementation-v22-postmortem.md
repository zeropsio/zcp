# v22 post-mortem and v8.82 implementation guide — the 98% push

**Status**: planning doc for v8.82. v8.81 just shipped (commit [69409b9](../internal/workflow/recipe_content_fix_gate.go)) closing three of the five structural gaps v22 surfaced. This doc specs the remaining five fixes, reasoning, and rollout order.

**Target**: v23 hits **A overall (pushing 98%)**. v7 gold-era A− was 95%; v22 was B (~85%). The plan here is calibrated to close the specific gaps and arrive at v7-era deliverable quality with the v8.78–v8.81 structural gate set intact.

---

## Table of contents

- [1. Where we are — v22 lived outcome vs v8.80 claimed guarantee](#1-where-we-are)
- [2. The gotcha-origin insight — why session-log feedback is the wrong shape](#2-the-gotcha-origin-insight)
- [3. The six-surface teaching system — map, boundaries, gaps](#3-the-six-surface-teaching-system)
- [4. Fixes for v8.82, ordered by blast-radius-per-effort](#4-fixes-for-v882)
  - [4.1 `gotcha_invariant_coverage` — plan-driven, not log-driven](#41-gotcha_invariant_coverage)
  - [4.2 `zerops_yml_comment_depth` — close the weakest-teaching gap](#42-zerops_yml_comment_depth)
  - [4.3 `content-quality-overview` topic — unify the six rubrics](#43-content-quality-overview-topic)
  - [4.4 Container-ops-in-README soft check — cleaner surface boundaries](#44-container-ops-in-readme-soft-check)
  - [4.5 IG causal-anchor rubric — symmetry with gotchas](#45-ig-causal-anchor-rubric)
- [5. What v8.82 explicitly does NOT do](#5-what-v882-explicitly-does-not-do)
- [6. The invariant-candidate data structure](#6-the-invariant-candidate-data-structure)
- [7. Expected v23 grade — where the 98% comes from](#7-expected-v23-grade)
- [8. Order of operations](#8-order-of-operations)
- [9. Risk analysis + validation plan](#9-risk-analysis)
- [10. Non-goals](#10-non-goals)
- [Appendix A — File inventory of changes](#appendix-a)
- [Appendix B — Starting invariant-candidate corpus](#appendix-b)

---

## 1. Where we are

### v22 scoreboard (one-page TL;DR)

- **Overall grade: B** (v20 was A−, v21 was D)
- **Wall: 103 min** (+45% vs v20's 71 min, −20% vs v21's 129 min)
- **Assistant events: 410** (new complete-run record, +40% vs v20's 294)
- **Tool calls: 243** (+37% vs v20's 177)
- **Close review: 0 CRIT / 0 WRONG** (cleanest since v14's spotless close)
- **Deploy-step runtime CRITs: 3** (NATS URL, S3 301, workerdev dist/main.js) — all fixed in main
- **Published tree: 6.1 MB** (v21: 214 MB — v8.80 `scaffold_hygiene` gate held)
- **Content: 7/6/6 gotchas, 8/6/6 IG items** — matches/exceeds v20 peak counts
- **BUT — 19/19 gotchas classified as run-incident-derived** (correlation audit, exact error-string matches to session log)
- **AND — cross-codebase coherence: C+ (the content-grade limiter)** — three README islands with sparse bridges

### v8.80 gate coverage against v22 evidence

| Gate from v8.80 | Held? | Evidence |
|---|---|---|
| §3.1 `scaffold_hygiene` runtime check | ✅ | Caught leak on api+worker, forced cleanup, published 6.1 MB |
| §3.2a bash-guard on zcp-side `cd /var/www` | ✅ (passive) | Main never attempted the pattern |
| §3.3 `gotcha_depth_floor` | ✅ (passive) | 7/6/6 exceeds floors |
| §3.4 `claude_readme_consistency` pattern rewrite | ✅ | Fired 4+4× on api+worker, caught TypeORM synchronize in CLAUDE.md |
| §3.5 framework-token purge | ✅ | Gotcha classification clean |
| §3.6d writer-subagent dispatch gate | ✅ dispatch / ❌ follow-through | Writer dispatched; post-writer iteration leaked into main |
| §3.6e MCP schema rename hints | ✅ | 0 schema errors (v21: 6) |
| §3.6f scaffold-reference content-diff | ✅ | Scaffold output close to `nest new` baseline |
| §3.7a pkill self-kill classifier | ✅ | 0 exit-255 failure classifications (v21: 6) |

**8 of 9 gates held structurally.** The one gap (§3.6d's post-dispatch leak) was closed in v8.81 §4.1 via the content-fix dispatch gate.

### v8.81 just shipped (commit 69409b9)

| Fix | What it closed |
|---|---|
| §4.1 post-writer content-fix dispatch gate | The Phase-4 15-minute leak (11 Edits on workerdev/README.md in main) |
| §4.6 `recipe_architecture_narrative` finalize check | Root README's "link aggregator, not architecture narrator" regression (the C+ coherence limiter) |
| §4.5 dev-start vs buildCommands contract check | Prevents workerdev `dist/main.js`-missing CRIT class |
| §4.3 NATS URL-embedded creds scaffold-brief preamble | Prevents v21→v22 recurrence of `TypeError: Invalid URL` |
| §4.4 S3 `storage_apiUrl` scaffold-brief preamble | Prevents S3 301 HTTP→HTTPS redirect CRIT class |

### What's left — the three structural gaps v8.81 didn't close

1. **The gotcha-origin asymmetry.** All 19 v22 gotchas were run-incident-derived; checks reward that shape and don't reward platform-invariant coverage. [Closed by §4.1 of this doc.]

2. **`zerops.yaml` comment depth is untaught and unchecked.** Integration Guide #1 copies zerops.yaml verbatim; weak comments there become weak IG comments, no retroactive deepening. [Closed by §4.2.]

3. **Six uncoordinated rubrics, no unified teaching.** The agent context-switches across six content surfaces during one run. No single overview topic names the full system. [Closed by §4.3.]

Plus two smaller gaps ([§4.4](#44-container-ops-in-readme-soft-check), [§4.5](#45-ig-causal-anchor-rubric)) that sharpen boundaries without heavy new infrastructure.

---

## 2. The gotcha-origin insight

### The problem (user's observation, validated)

v22 shipped 19/19 gotchas whose error strings appear verbatim in the session log. Gotcha-to-session-log correlation audit:

- apidev 7/7 — `TypeError: Invalid URL`, S3 `HeadBucketCommand` 301, `DB_*` `ECONNREFUSED`, Valkey env-name divergence, `QueryFailedError: relation "items"`, Meilisearch `search_masterKey` `MeiliSearchApiError`, `seed.ts` `execOnce` burn — every one matches exact session-log incident
- appdev 6/6 — `serviceStackIsNotHttp`, `sh: 1: npm: not found`, CORS preflight, `SyntaxError: Unexpected token '<'`, SSHFS+chokidar, `httpSupport` per-replica
- workerdev 6/6 — monotonic `processed_at` drift, SIGTERM drop, `ECONNREFUSED`, worker `serviceStackIsNotHttp`, `prepareCommands` bloat, `JobPayload` drift

Every gotcha is platform-anchored and passes `gotcha_causal_anchor` at token level. But their **origin** is "what happened during this run" — reproducible-post-mortem shape, not platform-knowledge shape.

### First design attempt — and why it was a Goodhart trap

My initial §4.2 proposal: plumb session-log access into the check runner, scan `tool_result` bodies for error strings, fail gotchas whose error tokens appear in the log.

**Four structural problems with that design:**

1. **It measures the wrong thing.** A gotcha that uses the canonical error string (`AUTHORIZATION_VIOLATION`, `TypeError: Invalid URL`) is *correct* whether the agent hit it this run or not. Penalizing its presence in the log penalizes correct content.
2. **It creates a perverse incentive.** Agent learns "avoid quoting exact error strings the session log contains." Gotchas drift to weaker symptom descriptions to pass the filter — the opposite of what `gotcha_causal_anchor` wants. Checks become adversarial.
3. **It doesn't reward invariants, it punishes incidents.** If 17/19 gotchas get flagged, the rational agent move is to rewrite them to be less specific. Quality goes down, not up.
4. **Session-log plumbing invites telemetry-driven-check proliferation.** Once the check runner reads run telemetry, every future check becomes tempted to consume it. The system compounds complexity on the wrong axis.

### The inversion — positive coverage, not negative filter

The problem isn't "too many incident gotchas" — it's "not enough invariant gotchas." Invariants are asymmetrically harder to write:

- Incident gotcha cost: ~free (narrate what you debugged)
- Invariant gotcha cost: research + engineering to match rubric

Checks reward the cheap shape, don't additionally reward the expensive shape. Agent's shortest path to green: incidents only.

**The fix is positive-signal, not negative-filter.** Compute the expected invariant-candidate set from the *plan* (not the log — the plan), check whether gotchas cover each candidate, require a minimum coverage. An incident gotcha that uses canonical error strings STILL COUNTS toward the coverage; origin doesn't matter, topical coverage does.

```
WRONG (Goodhart): "Your gotcha X contains a string from your session log — reject."
RIGHT (coverage):  "For your NATS + Meilisearch + Postgres stack, here are 5 known invariants.
                    At least 3 must be covered. We don't care if you hit them in this run."
```

This inverts the failure mode: incident-origin becomes irrelevant; what matters is whether the gotcha set cross-sects the *platform's surface for this stack*, not this run's path through it.

### Why "plan-derived, not log-derived" is the correct structural choice

- **Plan data is stable across runs.** The invariant-candidate set is a function of the declared stack, not agent stochasticity.
- **Plan data is observable at check time.** Already threaded through `RecipePlan` — no new infrastructure.
- **Plan data doesn't leak run telemetry into content quality.** No Goodhart-trap surface.
- **Candidates can be overridden with attestation.** If a candidate doesn't apply (e.g., `reconnect-forever` N/A because the recipe uses a synchronous Redis pattern not NATS), agent attests "N/A because X" — principled skip.

---

## 3. The six-surface teaching system

### Compact map

| # | Surface | Author | Step | Substep | Nature |
|---|---|---|---|---|---|
| 1 | `zerops.yaml` + inline comments | Main | Generate | `zerops-yaml` | Authored from scratch after smoke-test |
| 2 | IG (README fragment) | Main | Deploy | `readmes` | Authored from debug rounds + #1 verbatim as IG #1 |
| 3 | Gotchas (README fragment) | Main | Deploy | `readmes` | Authored from debug rounds + invariant-candidates |
| 4 | `import.yaml` env comments (×6) | Main | Finalize | — | Structured JSON input → auto-rendered YAML |
| 5 | Root README narrative | Main | Close | — | Authored prose + auto-templated skeleton |
| 6 | `CLAUDE.md` per codebase | Main | Deploy | `readmes` | Authored alongside README |

### Content-flow invariants (what must be true across surfaces)

```
 ┌────────────────────────────────────────────────────────────────┐
 │  [1] zerops.yaml (generate, main agent)                         │
 │      inline comments on fields                                  │
 │      written with live smoke-test context                       │
 │      ─── today: NO comment-quality check (weakest teaching)     │
 └─────────────────────────┬──────────────────────────────────────┘
                           ▼ verbatim copy
 ┌────────────────────────────────────────────────────────────────┐
 │  [2] IG #1 (deploy/readmes, main agent)                         │
 │      IG #1 copies zerops.yaml verbatim → shallow comments here  │
 │      inherit directly from shallow comments at [1]              │
 │      IG #2+ narrate debug-round diffs                           │
 │      ─── today: ig_per_item_standalone checks code-block only   │
 └─────────────────────────┬──────────────────────────────────────┘
                           ▼
 ┌────────────────────────────────────────────────────────────────┐
 │  [3] Gotchas (deploy/readmes, main agent)                       │
 │      "what surprised me when I DIDN'T change something"         │
 │      distinct from [2]; cross-codebase unique                   │
 │      ─── today: origin asymmetry (see §2)                       │
 └─────────────────────────┬──────────────────────────────────────┘
                           ▼
 ┌────────────────────────────────────────────────────────────────┐
 │  [6] CLAUDE.md (deploy/readmes, main agent)                     │
 │      repo-local ops; must NOT contradict [3]'s gotchas          │
 │      ─── today: claude_readme_consistency (v8.80) enforces      │
 │      ─── today: no check on container-ops-in-README placement   │
 └─────────────────────────┬──────────────────────────────────────┘
                           ▼
 ┌────────────────────────────────────────────────────────────────┐
 │  [4] env import.yaml (finalize, main agent, via JSON input)     │
 │      per-tier scaling + availability; self-contained per env    │
 │      ─── today: strongest rubric (35% reasoning markers, Jaccard) │
 └─────────────────────────┬──────────────────────────────────────┘
                           ▼
 ┌────────────────────────────────────────────────────────────────┐
 │  [5] Root README (close, main agent + finalize template)        │
 │      architecture overview; cross-codebase contracts            │
 │      ─── today: recipe_architecture_narrative (v8.81)           │
 └────────────────────────────────────────────────────────────────┘
```

### Boundaries, explicit

| Boundary | Rule | Current enforcement |
|---|---|---|
| #1 vs #4 | zerops.yaml = FRAMEWORK × PLATFORM per-service. import.yaml = ENV × SCALING per-tier. | By construction, separate files |
| #2 vs #3 | IG = "what I changed". Gotcha = "what surprises you if you don't". | `gotcha_distinct_from_guide` token-compare |
| #3 vs #6 | Platform facts = README. Repo-local ops = CLAUDE.md. | Stated recipe.md:2169 — **not programmatically checked** [§4.4] |
| #2 vs #4 | IG = integrator's view of zerops.yaml. env-comments = deployer's view of import.yaml. | Disjoint surfaces |
| #5 vs #2 | Root = architecture overview. Per-codebase = integration guide + gotchas. | `recipe_architecture_narrative` (v8.81) |

### Quality rubric strength, ranked

1. **Strongest: #3 Gotchas.** Explicit authenticity rubric (mechanism + failure-mode), 5 dedicated checks, per-role count floor, worker production-correctness mandatory.
2. **Strong: #4 env comments.** Explicit 35% reasoning-marker floor, per-service Jaccard distinctness, taxonomy of acceptable reasoning verbs published.
3. **Medium: #6 CLAUDE.md.** Byte floor + custom-section floor + README-consistency check.
4. **Medium: #5 root README.** Architecture narrative required but the rubric is permissive (names + ≥1 contract verb).
5. **Weak: #2 IG.** Only per-item code-block floor; no causal-anchor rubric for IG items. [§4.5]
6. **Weakest: #1 zerops.yaml comments.** No published rubric, no check. Taught by example only. [§4.2]

### Three gaps v8.82 must close

1. **Gotcha-origin asymmetry** — §4.1 invariant-coverage check.
2. **zerops.yaml commenting untaught + unchecked** — §4.2 comment-depth check mirrored from import.yaml.
3. **Six uncoordinated rubrics** — §4.3 `content-quality-overview` eager topic.

Plus boundary-sharpening (§4.4 container-ops-in-README) and rubric symmetry (§4.5 IG causal-anchor).

---

## 4. Fixes for v8.82

Each section has: **Why** (root cause it addresses), **What to change** (file:line + code shape), **Tests** (written RED first), **Acceptance** (what v23 must show), **Risks** (false-positive / design hazard / mitigation).

### 4.1 `gotcha_invariant_coverage`

**Why**: The single highest-ROI fix, closing the v22 gotcha-origin asymmetry documented in §2. Currently the check suite rewards incident gotchas (causal_anchor naturally matches them) and permits-but-doesn't-reward invariants (no additional check). v22 shipped 19/19 incident gotchas because that's the shortest path to the floor.

**Design principle**: the check fires on a PLAN-DERIVED set of invariant candidates, not on session-log content. Rewards coverage, doesn't punish origin.

**What to change**:

A. New file [`internal/workflow/invariant_candidates.go`](../internal/workflow/invariant_candidates.go) — the candidate corpus. Framework-agnostic in structure; data-driven per-service-category. Full candidate list in [Appendix B](#appendix-b). Shape:

```go
package workflow

// InvariantCandidate is a framework × platform intersection that a
// porting reader would plausibly hit on Zerops, regardless of whether
// THIS recipe's build surfaced it. Candidates are derived from the
// recipe plan's declared stack (service categories + runtime type),
// not from session-log telemetry.
type InvariantCandidate struct {
    // ID for attestation ("N/A because X" skip path).
    ID string
    // Human-readable short description for fail messages.
    Description string
    // Token(s) whose presence in a codebase's knowledge-base
    // fragment counts as "covered". ANY match = covered.
    CoverageTokens []string
    // Predicate: does this candidate apply to the given plan + host?
    Applies func(plan *RecipePlan, host string, role string) bool
}

// InvariantCandidatesFor returns the candidates that apply to a
// specific codebase within the plan. The predicate logic encodes:
//
//   - managed service categories the codebase exercises (DB, cache,
//     queue, storage, search) — each has its own base candidate set
//   - runtime characteristics (HTTP service vs headless worker,
//     SPA vs SSR, interpreter vs compiled)
//   - framework-family signals from plan.Research (ORM named, bundler
//     named, etc.) — but NEVER framework-brand exact match; candidates
//     must be "TypeORM/Prisma/Sequelize any-ORM" not "TypeORM specifically"
func InvariantCandidatesFor(plan *RecipePlan, host string, role string) []InvariantCandidate {
    // Body: concatenate the applicable base sets.
}
```

B. New file [`internal/tools/workflow_checks_gotcha_invariant_coverage.go`](../internal/tools/workflow_checks_gotcha_invariant_coverage.go). Shape:

```go
// checkGotchaInvariantCoverage fires per codebase at deploy-step readmes
// completion. Matches each applicable invariant candidate against the
// codebase's knowledge-base fragment. Passes when coverage ≥ minimum
// OR unmatched candidates are all explicitly attested "N/A because X".
//
// Coverage rule: a candidate is "covered" if ANY of its CoverageTokens
// appears (case-insensitive substring match) anywhere in the knowledge-
// base fragment OR in the codebase's zerops.yaml comments. The zerops.yaml
// extension recognizes that a reader who reads the IG #1 (which is
// zerops.yaml verbatim) gets equivalent coverage.
//
// Minimum: ceil(0.5 * applicableCount) for the codebase. Example: if 6
// candidates apply to apidev, 3 must be covered (or attested N/A).
//
// Attestation escape hatch: a comment line in README like
//   <!-- INVARIANT-NA: reconnect-forever | uses synchronous Redis, no long-lived broker -->
// counts the candidate as "N/A acknowledged". This lets principled skips
// pass while keeping silent omissions gated.
func checkGotchaInvariantCoverage(readmeContent, zeropsYml string, plan *workflow.RecipePlan, host, role string) []workflow.StepCheck {
    // Body.
}
```

C. Register in [`internal/tools/workflow_checks_recipe.go`](../internal/tools/workflow_checks_recipe.go) `checkRecipeDeployReadmes`, alongside existing per-codebase checks. Fires at deploy-step completion.

D. Update recipe.md readmes sub-step guidance to mention:
- The invariant-coverage check exists and how to satisfy it
- The `INVARIANT-NA:` attestation marker syntax
- A pointer to the candidate list (via new topic `invariant-candidates-for-stack`, see §4.3)

**Tests** (new file `internal/tools/workflow_checks_gotcha_invariant_coverage_test.go`):

```go
func TestGotchaInvariantCoverage_AllCandidatesCovered_Passes(t *testing.T)
func TestGotchaInvariantCoverage_BelowCoverageFloor_Fails(t *testing.T)
func TestGotchaInvariantCoverage_NATokenInAttestation_Passes(t *testing.T)
func TestGotchaInvariantCoverage_ZeropsYmlCommentsCountTowardCoverage(t *testing.T)
func TestGotchaInvariantCoverage_NoApplicableCandidates_SkipsCheck(t *testing.T)
func TestGotchaInvariantCoverage_PartialCoverageWithNAAttestations_Passes(t *testing.T)
func TestGotchaInvariantCoverage_FailureDetailNamesUncoveredCandidates(t *testing.T)

func TestInvariantCandidatesFor_NATSStack_IncludesReconnectForever(t *testing.T)
func TestInvariantCandidatesFor_ObjectStorageStack_IncludesEndpointHTTPSPreference(t *testing.T)
func TestInvariantCandidatesFor_HeadlessWorker_IncludesServiceStackIsNotHttp(t *testing.T)
func TestInvariantCandidatesFor_NoCache_DoesNotIncludeCacheCandidates(t *testing.T)
```

**Acceptance**: v23's recipe must demonstrate at least one gotcha whose mechanism token appears in the candidate list AND whose error string does NOT appear in the session log (proof of invariant origin). The check's failure detail message, when it fires, must list the specific uncovered candidates + the `INVARIANT-NA:` skip syntax.

**Risks**:

- **False positives from over-broad candidates.** Mitigation: start with conservative minimum (ceil(0.5 × applicable)); allow agent override via attestation; informational-only failure-mode for first 2 versions (v8.82–v8.83) to collect real-world false-positive data before making hard-gate.
- **Candidate list staleness.** Mitigation: candidate set is data (not code logic), updated alongside recipe-version-log findings. New intersections surface → append to corpus. Quarterly review tied to version-log milestones.
- **Framework hardcoding creep.** Mitigation: ALL candidates must pass a meta-test that their predicates don't reference any specific framework brand (no "TypeORM", "Laravel", "Next.js" — only "ORM present", "server-side framework", "bundler-backed SPA"). This extends the v8.80 framework-token purge to the candidate corpus.

### 4.2 `zerops_yml_comment_depth`

**Why**: Second-highest ROI. v22 and earlier runs had no rubric and no check on zerops.yaml comments. IG #1 copies zerops.yaml verbatim into the published README — so shallow comments at #1 become shallow IG #1 with no retroactive deepening. The deepest content surface (published to users) inherits the weakest teaching surface.

**Design**: mirror [`workflow_checks_comment_depth.go`](../internal/tools/workflow_checks_comment_depth.go) (the env-comments check), applied to each codebase's `zerops.yaml`. Same reasoning-marker taxonomy (`because`, `otherwise`, `without`, `must`, `rather than`, etc.), same 35% floor, same ≥2 absolute-minimum.

**What to change**:

A. New file [`internal/tools/workflow_checks_zerops_yml_depth.go`](../internal/tools/workflow_checks_zerops_yml_depth.go). Extract the reasoning-marker detection logic from `workflow_checks_comment_depth.go` into a shared `pkg/depth.go` if the copy would exceed 3 functions; otherwise straight copy with the import-yaml-specific logic stripped.

```go
// checkZeropsYmlCommentDepth validates that each codebase's zerops.yaml
// carries reasoning-anchored comments at a floor parity with the env
// import.yaml comment check (35% of substantive blocks contain a
// reasoning marker; ≥2 reasoning-comments absolute minimum). zerops.yaml
// is the source that IG #1 copies verbatim — weak zerops.yaml comments
// produce weak IG #1 by direct inheritance, with no later surface to
// retroactively add the why.
//
// The reasoning-marker corpus is identical to env-comments (because/
// otherwise/without/must/rather than/instead of/so that/prevents/
// leads to/mandatory/required/at build time/at runtime/rolling/drain).
// Same taxonomy, same floor, different file.
func checkZeropsYmlCommentDepth(zeropsYml, host string) []workflow.StepCheck
```

B. Register in `checkRecipeDeployReadmes` (or move to generate-step if the agent should fail BEFORE writing IG — actually deploy is fine because the IG copy happens at readmes, not at generate).

Actually important nuance: the check must fire at **deploy-step `readmes` completion**, not at generate-complete. Why? Because the IG is authored at deploy/readmes; if we fire the comment-depth check at generate, the agent fixes zerops.yaml comments then but by the time IG is written (much later), the zerops.yaml may have been re-edited. Firing at readmes-complete ensures the version copied into IG is the one checked.

Wait — actually the IG copies zerops.yaml *at write time*, so if we fire at generate-complete, the agent fixes zerops.yaml comments before deploy starts. That's ideal — the deployed zerops.yaml already has good comments, and IG #1 (written later) copies the good version. So: **fire at generate-complete**, in the existing `checkRecipeGenerate` flow.

C. Update recipe.md `zerops-yaml-header` block (lines 499–520) and/or `comment-anti-patterns` to mention the depth check and reasoning-marker expectations.

**Tests**:

```go
func TestZeropsYmlCommentDepth_SufficientReasoning_Passes(t *testing.T)
func TestZeropsYmlCommentDepth_FieldNarrationOnly_Fails(t *testing.T)
func TestZeropsYmlCommentDepth_NoComments_Fails(t *testing.T)
func TestZeropsYmlCommentDepth_MultiSetupFile_AnalyzesAllSetups(t *testing.T)
func TestZeropsYmlCommentDepth_HardFloorOfTwo_EnforcedEvenAboveRatio(t *testing.T)
```

**Acceptance**: v23 every codebase's zerops.yaml ships with ≥35% of substantive comments containing at least one reasoning marker. IG #1 in each README therefore inherits that depth.

**Risks**:

- **Regression against conciseness preference.** Some engineers prefer terse YAML. Mitigation: the rubric already exists and is accepted for import.yaml; parity is not a new aesthetic imposition. If complaints surface, we can add a `# no-depth-check` per-block opt-out comment (but resist adding until needed).
- **IG #1 getting out-of-sync with zerops.yaml between check-time and copy-time.** Mitigation: the existing `readmes` sub-step validator already requires IG #1 to be a verbatim copy; any drift would trigger re-validation. Safe.

### 4.3 `content-quality-overview` topic

**Why**: The six surfaces each have their own rubric, their own teaching location in recipe.md, their own check suite. The agent context-switches across six frameworks during one run without ever seeing them as a unified system. This is the structural reason for surface-crossing mistakes: CLAUDE.md using a pattern README forbids, gotchas restating IG headings, env comments template-leaking. Each mistake is caught by its own cross-boundary check, but the agent wasn't told the system has boundaries.

**Design**: add a new eager-injected topic at deploy-step start (before the `readmes` sub-step) that names all six surfaces, their order, their authors, their boundaries, their rubrics. Purely a teaching/coherence topic — no check, no enforcement. The agent reads it once at deploy-step start and has the mental model for the rest of the run.

**What to change**:

A. New block in [`internal/content/workflows/recipe.md`](../internal/content/workflows/recipe.md): `<block name="content-quality-overview">`. Content should reproduce the six-surface table from [§3](#3-the-six-surface-teaching-system) of this doc in agent-facing prose, plus the content-flow diagram. Keep it under 150 lines — it's an overview, not a rubric replay. Point to each individual topic/block for the full per-surface rubric.

B. Register in [`internal/workflow/recipe_topic_registry.go`](../internal/workflow/recipe_topic_registry.go):

```go
{
    ID: "content-quality-overview", Step: RecipeStepDeploy,
    Description: "Six-surface teaching system — what goes where, written when, by whom, to what rubric (eager)",
    BlockNames:  []string{"content-quality-overview"},
    Eager:       true, // injected into deploy-step detailedGuide
    Predicate:   isShowcase, // or: always; decide at implementation time
},
```

Eager injection is important — the v19 post-mortem analysis showed that topics the agent doesn't fetch are topics the agent doesn't know exist. This one is load-bearing enough to warrant automatic injection.

C. Register in [`internal/workflow/recipe_section_catalog.go`](../internal/workflow/recipe_section_catalog.go) alongside the other deploy-step sections.

D. Add a test in [`internal/workflow/recipe_topic_registry_test.go`](../internal/workflow/recipe_topic_registry_test.go):

```go
func TestRecipeTopicRegistry_ContentQualityOverview_Registered(t *testing.T)
func TestInjectEagerTopics_ContentQualityOverview_InDeployShowcase(t *testing.T)
```

**Acceptance**: v23 session log must contain the `content-quality-overview` topic ID in eager-injected deploy-step guidance. No agent fetch needed (it's eager). Pass criterion is structural: the topic exists and reaches the agent.

**Risks**:

- **Brief bloat at deploy step.** Mitigation: keep the overview under 150 lines; replace per-surface deep-dive blocks from eager injection if they double-up. The overview POINTS AT them; they don't both need to be eager.
- **Content becoming stale as individual surface rubrics evolve.** Mitigation: the overview names topics + checks by ID but keeps per-surface detail high-level. Individual rubrics continue to live in their own blocks; the overview doesn't try to replicate them.

### 4.4 Container-ops-in-README soft check

**Why**: recipe.md:2169 explicitly says "Container-ops content…goes in CLAUDE.md, NOT in README.md gotchas." The rule is stated but not enforced. v22 workerdev README includes `prepareCommands` bloat as a gotcha (bordering on container-ops) while CLAUDE.md correctly has the SSHFS / fuser content. Boundaries slip when rules aren't checked.

**Design**: soft check (status: `info`, not `fail`) that emits when README gotchas contain container-ops tokens that belong in CLAUDE.md. Informational only — nudges the agent, doesn't block the step. Avoids false-positive friction while still surfacing boundary drift.

**What to change**:

A. New file [`internal/tools/workflow_checks_readme_container_ops.go`](../internal/tools/workflow_checks_readme_container_ops.go). Token corpus:

```go
var containerOpsTokens = []string{
    "sshfs",
    "fuser",
    "sudo chown",
    "uid 2023", // Zerops container UID
    "zcli vpn up",
    "npx tsc resolves", // the npx-wrong-package trap
    ".ssh/known_hosts",
    "ssh zerops@",
    "ssh-keygen",
    // etc.
}

func checkReadmeContainerOps(readmeContent, host string) []workflow.StepCheck {
    // Find each gotcha bullet (line starting with `- **` inside
    // knowledge-base fragment markers). For each, scan for token
    // matches. Emit one info-status check per matched bullet naming
    // the token + suggesting CLAUDE.md as the correct location.
}
```

Return `statusInfo` (new constant — check the existing status corpus for info/warning shapes; if none, introduce `statusInfo = "info"` alongside `statusPass` / `statusFail`).

B. Register in `checkRecipeDeployReadmes`.

C. The finalize checker should treat `statusInfo` as non-blocking (don't flip `allPassed` to false).

**Tests**:

```go
func TestReadmeContainerOps_SSHFSInGotcha_EmitsInfo(t *testing.T)
func TestReadmeContainerOps_FuserInGotcha_EmitsInfo(t *testing.T)
func TestReadmeContainerOps_PlatformOnlyGotcha_NoEmit(t *testing.T)
func TestReadmeContainerOps_InfoStatusDoesNotBlockFinalize(t *testing.T)
```

**Acceptance**: v23 emits zero `info`-status `readme_container_ops_nudge` findings — meaning the agent placed container-ops content correctly in CLAUDE.md on first pass. If findings emit, they inform without blocking; agent can fix or leave.

**Risks**:

- **Token false positives.** A gotcha mentioning SSHFS as context for a platform failure mode ("our seed script times out on SSHFS") is legitimately a README gotcha. Mitigation: info-only status prevents false-positive blocking; agent judgment prevails. If false-positive rate exceeds 10% over 3 runs, tune the corpus.
- **Soft checks get ignored.** Mitigation: the info status is displayed in the checker result prominently; the rubric in recipe.md `content-quality-overview` (§4.3) names it explicitly as a nudge-not-gate. If ignore rate is persistently high over 5+ runs, escalate to `fail`.

### 4.5 IG causal-anchor rubric

**Why**: `gotcha_causal_anchor` requires every gotcha bullet to name a mechanism + a failure mode. IG has no analogous rubric — `ig_per_item_standalone` checks code-block presence + platform-anchor token in first paragraph, but doesn't require the step to NAME a specific mechanism or describe what it FIXES. Result: some IG items ship as "add this code" without naming what platform failure the code prevents.

**Design**: extend `ig_per_item_standalone` to also require each IG H3 heading or first paragraph to name a concrete failure mode the code fixes — mirroring the gotcha rubric's structure.

**What to change**:

A. Extend [`internal/tools/workflow_checks_per_item.go`](../internal/tools/workflow_checks_per_item.go) — the existing `checkIGPerItemStandalone` check — to additionally require a symptom-verb or error-token in the item's prose body.

Current rule (paraphrased from the agent's code exploration):
> Each `### N.` block must have ≥1 fenced code block AND a platform-anchor token in the first paragraph.

New rule (adds causal-anchor parity):
> Each `### N.` block must have ≥1 fenced code block AND a platform-anchor token AND a symptom token (HTTP status, quoted error name in backticks, or symptom verb: "rejects/deadlocks/drops/crashes/times out/returns wrong content-type").

B. The existing `concreteFailureSymptoms` corpus from `workflow_checks_causal_anchor.go` can be reused — extract to a shared helper if the import is awkward.

**Tests**:

```go
func TestIGPerItemStandalone_WithSymptomAndMechanism_Passes(t *testing.T)
func TestIGPerItemStandalone_MechanismButNoSymptom_Fails(t *testing.T)
func TestIGPerItemStandalone_CodeBlockAloneNoProseAnchor_Fails(t *testing.T) // existing
```

**Acceptance**: v23 every IG H3 block contains a mechanism-anchor AND a symptom-anchor in its prose body. Existing code-block-per-item check continues to pass.

**Risks**:

- **Legitimate "here's the shape" IG items that don't describe a failure.** E.g., IG #1 "Adding zerops.yaml" doesn't prevent a failure, it IS the config. Mitigation: grandfather IG #1 (the zerops.yaml item) — the check only fires for IG #2+. Or: require only symptom-anchor for items with mechanism tokens AFTER the zerops.yaml item (structural position, not content).

---

## 5. What v8.82 explicitly does NOT do

These are design choices made deliberately. Listed here to prevent scope creep on reading.

- **No session-log access from checks.** This was my first-draft §4.2. It's a Goodhart trap (§2). v8.82 stays at the plan + filesystem layer for all check inputs.
- **No new subagent dispatch gates.** v8.80 §3.6d (writer) + v8.81 §4.1 (content-fix) + v8.81 §4.8 proposal (3-split code review) cover the dispatch surface. Adding more gates risks over-dispatch cost. Measure first.
- **No framework-specific hardcoding.** All new code (especially §4.1 invariant candidates) must pass a meta-test that predicates don't reference framework brands. "NATS present" = yes; "NestJS present" = no.
- **No predecessor-floor re-introduction.** v8.78 rolled back predecessor-floor from gate to informational. §4.1 invariant-coverage is the correct replacement for quantitative pressure; predecessor-floor stays advisory.
- **No rubric-band additions to the O-dimension of the recipe grade.** v8.81 §4.7 proposed assistant-event + tool-call bands; deferred until we see Opus 4.7 baselines across 2–3 runs. Add rubric bands ≤ v8.83, not in v8.82, to avoid calibrating against one data point.
- **No retroactive fixes to v22's published artifacts.** The v22 recipe ships with 19/19 incident gotchas; we don't rewrite history. v8.82's checks are for v23 onward.

---

## 6. The invariant-candidate data structure

The §4.1 check's correctness depends on the candidate corpus. Design principles:

### 6.1 Each candidate must be framework-brand-agnostic

A candidate's predicate looks at `plan.Research`, `plan.Targets`, and managed-service categories — NEVER at `plan.Framework` directly. Examples:

- ✅ "Worker subscribed to a broker with ≥2 replicas needs exactly-once semantics" — predicate: `role == "worker" && hasManagedService(plan, "queue") && planAllowsScaling(plan)`
- ❌ "NATS queue groups" — too brand-specific; RabbitMQ, Kafka, Redis Streams all need equivalent patterns. The candidate is "broker subscription exactly-once", not "NATS queue group".
- ✅ CoverageTokens for the above: `["queue group", "consumer group", "exactly once", "per replica", "fan-out", "shared subscription"]` — any matches
- ❌ CoverageTokens: `["queue: 'workers'"]` — NATS-specific literal

This keeps the candidate corpus honest across future framework/service additions.

### 6.2 Applicability predicates compose from small observable facts

Each candidate has an `Applies(plan, host, role) bool` function that composes from:

- `role` (api / frontend / worker / fullstack)
- managed-service category presence (`hasManagedService(plan, "queue")`, `hasManagedService(plan, "cache")`, etc.)
- runtime characteristics (`hasHTTPPort(plan, host)`, `isHeadless(plan, host)`)
- build characteristics (`usesBundler(plan)`, `usesOrm(plan)`)
- scaling characteristics (`planAllowsScaling(plan)`)

These helpers already exist in workflow package or are trivial to add.

### 6.3 Coverage-token matching is substring, case-insensitive, multi-token

A candidate is "covered" if the codebase's knowledge-base fragment or zerops.yaml comments contain ANY ONE of the candidate's CoverageTokens (substring, case-insensitive). Multi-token lists accommodate phrasing variance — a gotcha that says "queue group" and another that says "consumer group" both match the broker-exactly-once candidate.

### 6.4 Minimum coverage floor: ceil(0.5 × applicable)

Half-coverage is the initial floor. Conservative to avoid false-positive blocking. If v23 data shows the floor is too loose, raise to 0.65 in v8.83. If too tight, lower to 0.4. Data-driven calibration.

### 6.5 Attestation escape hatch syntax

```markdown
<!-- INVARIANT-NA: broker-exactly-once | uses Redis Streams XREADGROUP which has native at-least-once semantics by consumer group -->
```

Parseable shape: `INVARIANT-NA:` + candidate ID + `|` + free-text reason. The check reads every such comment in the README and counts the named candidate as "N/A acknowledged" (covered for floor purposes). Reasons must be ≥20 chars to prevent `INVARIANT-NA: foo | x` no-op escape hatches.

### 6.6 Starting corpus — see [Appendix B](#appendix-b)

The starting corpus below covers the primary service categories and runtime shapes ZCP recipes target today. Every candidate has been cross-checked against the v7 → v22 version log to ensure it represents a real known intersection.

---

## 7. Expected v23 grade

### Per-axis expectations

| Axis | v20 (A−) | v22 (B) | v23 target (w/ v8.82) | Driver |
|---|---|---|---|---|
| S — Structural | A− | B | **A−/A** | §4.3–§4.5 of v8.81 prevent 3 recurrence CRITs; close review clean |
| C — Content | A− | B+ | **A** | §4.1 forces invariant-diverse gotchas; §4.2 forces deep zerops.yaml comments (which IG #1 inherits); §4.3 gives the agent coherent mental model; v8.81 §4.6 closes cross-codebase coherence |
| O — Operational | A− | B | **A−** | v8.81 §4.1 content-fix dispatch gate kills Phase-4 leak (~15 min saved); §4.3–§4.5 scaffold preambles prevent runtime CRIT recovery cycles (~5-10 min saved) |
| W — Workflow | A− | B | **A−/A** | All v8.80 + v8.81 + v8.82 gates hold; delegation shape intact; 3-split code review continues |

### Overall: **A (targeting 98%)**

The 98% target requires:

1. **All three grand rubrics cleared at A on first pass of each step** (not after 2–3 iterations).
2. **Invariant-coverage check fires <2 times** (most codebases satisfy on first authorship).
3. **Wall clock ≤90 min** — brings back toward v20's 71 min by eliminating Phase-4 leak + 3 runtime CRIT recovery cycles.
4. **Zero close-review CRITs**, zero deploy-time CRITs (v8.81 scaffold preambles should handle the 3 v22-class recurrences at scaffold time, not runtime).
5. **Gotcha-origin diversity**: ≥3 gotchas per codebase whose mechanism-anchor token doesn't appear as a literal in the session log. Proof-of-invariant coverage beyond the incident set.

If any of these miss, we're at A−/A, not A. The 2–3% wiggle room is where Opus 4.7 variance, unusual recipe stack, or a corner-case invariant-candidate gap would surface.

### Where 100% lives (not v8.82 territory)

The 2% gap from 100% would require:

- Rubric calibration with 3+ Opus 4.7 baselines (v8.83 work)
- Per-framework invariant candidate tuning as new recipe stacks come online
- Something we haven't anticipated (by definition out of scope — v8.83/84 post-mortem territory)

v8.82's job is 98%. Save the last 2% for when the data justifies the next calibration round.

---

## 8. Order of operations

### Phase ordering: least-risky first, highest-dependency last

1. **§4.2 `zerops_yml_comment_depth`** — first. Straight copy-pattern from `workflow_checks_comment_depth.go`. Lowest risk, highest confidence. Could ship as its own commit.

2. **§4.4 container-ops-in-README** — second. Soft check, info-only status. Low blast radius if token corpus needs tuning. Decouple from dependent work.

3. **§4.5 IG causal-anchor rubric** — third. Extends existing check with a new requirement. Medium complexity but well-scoped.

4. **§4.3 `content-quality-overview` topic** — fourth. Pure content addition to recipe.md + registry. No new Go logic; lowest implementation complexity but depends on the above being done (references their checks).

5. **§4.1 `gotcha_invariant_coverage`** — LAST. Most design work. The candidate corpus (Appendix B) needs careful review. Depends on §4.3 for agent-facing overview. Ship behind an informational-only flag for v8.82; escalate to hard-gate in v8.83 after one run of real-world false-positive data.

### Within each phase

Standard TDD rhythm per CLAUDE.md: RED test → minimal implementation → GREEN → refactor. Each phase = one commit + one test run + one lint pass before moving to the next. No phase-skipping — if tests don't pass, fix before the next phase starts.

### Estimated effort

| Phase | Effort | Files touched |
|---|---|---|
| §4.2 | ~2 hours | 2 new + 1 modified |
| §4.4 | ~1.5 hours | 2 new + 1 modified |
| §4.5 | ~1 hour | 1 modified + 1 test file |
| §4.3 | ~2 hours | 1 modified (recipe.md) + 2 modified (registry + catalog) + 1 test file |
| §4.1 | ~4–6 hours | 2 new Go files + 2 test files + 1 modified registration + Appendix B corpus |

Total: ~11–13 hours of implementation. Release tag v8.90.0 estimated (following the current minor-bump cadence).

---

## 9. Risk analysis

### Risks shared across v8.82

- **Check proliferation cognitive load.** Each new check is another failure message the agent has to read + act on. Mitigation: `content-quality-overview` (§4.3) names the full check list up front; checks' failure messages point to the overview for context.

- **Iteration cost inflation.** Every new gated check adds potential iteration rounds. Mitigation: measure time-to-pass per check in v8.82 pilot runs; if any check adds >2 minutes average, tune the rubric or demote to informational.

- **Framework-brand hardcoding creep.** Invariant-candidate corpus (Appendix B) is the hot spot. Mitigation: meta-test that scans candidate predicates for framework-brand tokens.

### Per-check risks (see each §4.N section above)

### Rollback plan

Every check introduced in v8.82 carries a feature flag or informational-first pattern:

- §4.1 ships **informational-only** (emits findings, doesn't block step completion) in v8.82. Escalate to hard-gate in v8.83 if false-positive rate < 5%.
- §4.2 ships **hard-gate** immediately (low-risk, direct copy from proven check).
- §4.3 has no rollback surface (it's content + eager topic, no check).
- §4.4 ships **info-only** (soft check by design).
- §4.5 ships **hard-gate** immediately (extends existing check with clear shape).

If v23 data reveals v8.82 needs rollback for any check, the rollback is a single-line status-constant change (`statusFail` → `statusInfo`) per check — no schema migration, no state-cleanup overhead.

### Validation plan

- **Unit tests**: all new checks have ≥5 tests each (pass path, fail path, edge, skip, corpus coverage).
- **Integration tests**: extend `integration/recipe_end_to_end_test.go` with fixtures demonstrating each check fires at its intended sub-step.
- **Shadow-test against v22 + v20 artifacts**: run the new checks against the actual published outputs of v20 (A−) and v22 (B) in repo. Expected results:
  - v20 should PASS §4.2 (zerops.yaml comments were decent) and FAIL §4.1 (gotchas were decorative but not invariant-anchored) — this is the v20 latent issue we're now surfacing.
  - v22 should FAIL §4.1 (19/19 incident gotchas) and PARTIALLY PASS §4.2 (zerops.yaml comments were present, depth variable).
  - If v20 fails §4.1 too harshly (e.g., full block), tune the floor down.

- **Live recipe run (v23)**: ONE showcase recipe run with v8.82 shipped, analyzed like v22 (TIMELINE + session analysis + content audit). If v23 meets expected-grade criteria from §7, v8.82 passes acceptance.

---

## 10. Non-goals

Things NOT to do in v8.82, for discipline:

1. **Don't re-architect the check plumbing.** The `RecipeStepChecker` signature + `StepCheck` result shape is fine. New checks slot into the existing pattern.
2. **Don't add new sub-step constants to the workflow state machine.** The v8.81 content-fix gate operates at step-level retry detection, not as a new sub-step. Keep the substep list stable.
3. **Don't extend recipe.md beyond ~150 lines net.** The file is already 2000+ lines. Discipline by compression: the `content-quality-overview` block is a pointer-aggregator, not a rubric replay.
4. **Don't touch the knowledge theme files in `/internal/knowledge/themes/`.** v8.82 ships entirely in check logic + recipe.md + registry. Theme content evolves separately and is tied to real-world usage observations, not per-version post-mortems.
5. **Don't change the recipe-version-log grading rubric.** v8.82 is calibrated against the current S/C/O/W framework. Rubric changes (e.g., adding event-count bands) are separate design work in v8.83.
6. **Don't ship ≥2 new hard gates in one release.** §4.2 is the only new hard gate in v8.82; §4.1 ships informational-first. This keeps the v8.80→v8.81→v8.82 gate-addition cadence conservative.

---

## Appendix A — File inventory of changes

### New files

| Path | Purpose | Size estimate |
|---|---|---|
| `internal/workflow/invariant_candidates.go` | §4.1 candidate corpus + `InvariantCandidatesFor` | 300–450 lines |
| `internal/workflow/invariant_candidates_test.go` | Candidate predicate tests + meta-test for framework-agnosticism | 150–200 lines |
| `internal/tools/workflow_checks_gotcha_invariant_coverage.go` | §4.1 check + coverage matcher | 150–200 lines |
| `internal/tools/workflow_checks_gotcha_invariant_coverage_test.go` | §4.1 check tests | 200–250 lines |
| `internal/tools/workflow_checks_zerops_yml_depth.go` | §4.2 zerops.yaml comment-depth check | 100–150 lines |
| `internal/tools/workflow_checks_zerops_yml_depth_test.go` | §4.2 tests | 150–200 lines |
| `internal/tools/workflow_checks_readme_container_ops.go` | §4.4 container-ops soft check | 100 lines |
| `internal/tools/workflow_checks_readme_container_ops_test.go` | §4.4 tests | 100 lines |

### Modified files

| Path | Change |
|---|---|
| `internal/tools/workflow_checks_recipe.go` | Register §4.1, §4.2, §4.4 in appropriate step |
| `internal/tools/workflow_checks_per_item.go` | Extend §4.5 with symptom-anchor rule |
| `internal/tools/workflow_checks_per_item_test.go` | Add §4.5 symptom-anchor tests |
| `internal/content/workflows/recipe.md` | Add `content-quality-overview` block; update existing blocks to reference new checks |
| `internal/workflow/recipe_topic_registry.go` | Register §4.3 topic |
| `internal/workflow/recipe_section_catalog.go` | Add `content-quality-overview` to deploy sections |
| `internal/workflow/recipe_topic_registry_test.go` | Assertions for §4.3 topic registration + eager injection |

### Total change estimate

- **New Go**: ~1250–1550 lines (including tests)
- **New markdown** (recipe.md): ~150 lines
- **Modified Go**: ~100–200 lines
- **Tests added**: ~700–850 lines

Comparable scope to v8.80 (the v21 post-mortem implementation, ~2400 lines across 9 fixes).

---

## Appendix B — Starting invariant-candidate corpus

The corpus is ordered by service category, with runtime-type modifiers. Every candidate has been cross-referenced against v7–v22 recipe log entries to ensure it's a real observed intersection, not speculation.

### B.1 Database-backed (any ORM / any SQL/NoSQL driver)

```go
{
    ID: "db-migration-execonce-idempotency",
    Description: "Migrations wrapped in zsc execOnce ${appVersionId} — rerunning with same version skips; unbumped redeploy after failure won't retry",
    CoverageTokens: []string{"zsc execOnce", "execOnce", "${appVersionId}", "appVersionId", "idempotent migration", "idempotent re-seed", "burned key"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "db") && roleIsServer(role) },
},
{
    ID: "db-env-var-mapping-divergence",
    Description: "ORM expects DB_HOST/DB_PORT but Zerops injects ${db_hostname}/${db_port} — mismatch yields ECONNREFUSED 127.0.0.1",
    CoverageTokens: []string{"DB_HOST", "db_hostname", "${db_", "ECONNREFUSED", "camelCase", "db_dbName"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "db") && !isFrontendRole(role) },
},
{
    ID: "db-readinesscheck-pre-initcommands",
    Description: "Stage replica receives traffic before initCommands finish → QueryFailedError on first requests",
    CoverageTokens: []string{"readinessCheck", "initCommands", "race", "QueryFailedError", "relation does not exist"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "db") && hasHTTPPort(p, host) && planAllowsStageDeploy(p) },
},
```

### B.2 Cache (Redis / Valkey / Keydb)

```go
{
    ID: "cache-env-name-divergence",
    Description: "Keyv / node-redis / ioredis convention (REDIS_HOST) vs Zerops injection (${redis_hostname}) — silent undefined → cache always-miss",
    CoverageTokens: []string{"redis_hostname", "REDIS_HOST", "undefined", "always miss", "X-Cache", "keyv", "degrade silently"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "cache") && roleIsServer(role) },
},
{
    ID: "cache-no-auth-project-network",
    Description: "Valkey instances run without auth on project-internal network; Keyv/ioredis configured to require auth will throw NOAUTH",
    CoverageTokens: []string{"NOAUTH", "no auth", "no password", "project-network", "isolation"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "cache") && roleIsServer(role) },
},
```

### B.3 Broker (NATS / RabbitMQ / Redis Streams / any message queue)

```go
{
    ID: "broker-exactly-once-group-subscription",
    Description: "Multi-replica workers need broker group/shared-subscription semantics or every replica processes every message (double-process)",
    CoverageTokens: []string{"queue group", "consumer group", "shared subscription", "exactly once", "per replica", "double-process", "fan-out"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "queue") && role == "worker" && planAllowsScaling(p) },
},
{
    ID: "broker-sigterm-drain-in-flight",
    Description: "SIGTERM on rolling redeploy drops in-flight broker messages unless handler drains before exit",
    CoverageTokens: []string{"SIGTERM", "drain", "in-flight", "graceful shutdown", "onModuleDestroy", "nc.drain", "connection close"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "queue") && role == "worker" },
},
{
    ID: "broker-credentials-as-options-not-url",
    Description: "URL-embedded creds (amqp://user:pass@host) may fail on auto-generated passwords with URL-reserved chars — pass creds as separate ConnectionOptions fields",
    CoverageTokens: []string{"URL-embedded", "URL-reserved", "ConnectionOptions", "TypeError: Invalid URL", "separate fields", "credential-free URL"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "queue") && (role == "api" || role == "worker") },
},
{
    ID: "broker-reconnect-forever-for-long-lived",
    Description: "Long-lived broker consumer on a restart-capable container needs reconnect-forever; losing connection should exit and let supervisor restart, not silently fail",
    CoverageTokens: []string{"reconnect", "reconnectAttempts", "maxReconnectAttempts", "reconnect forever", "exit and restart"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "queue") && role == "worker" },
},
```

### B.4 Object storage (S3-compatible / MinIO)

```go
{
    ID: "storage-https-endpoint-redirect",
    Description: "HTTP endpoint returns 301 to HTTPS; AWS SDK doesn't follow redirects — HeadBucket throws NotFound",
    CoverageTokens: []string{"storage_apiUrl", "301", "redirect", "AWS SDK", "does not follow", "HeadBucket", "forcePathStyle"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "storage") && roleIsServer(role) },
},
{
    ID: "storage-bucket-name-prefixed",
    Description: "Auto-provisioned bucket has project-prefixed slug — hardcoding bucket name breaks across environments",
    CoverageTokens: []string{"storage_bucketName", "bucket name", "project-prefixed", "slug", "different between tiers"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "storage") && roleIsServer(role) },
},
```

### B.5 Search engine (Meilisearch / Typesense / Elasticsearch)

```go
{
    ID: "search-masterkey-scope",
    Description: "Multiple env keys published; only masterKey has write scope — defaultAdminKey / defaultSearchKey / defaultReadOnlyKey fail on createIndex / addDocuments",
    CoverageTokens: []string{"masterKey", "search_masterKey", "write scope", "403", "API key is invalid", "defaultSearchKey"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "search") && roleIsServer(role) },
},
{
    ID: "search-seed-idempotent-no-op-trap",
    Description: "ON CONFLICT DO NOTHING in seed SQL is idempotent for Postgres but hides trap: if first-deploy seed failed partway, subsequent execOnce redeploys skip it entirely — DB rows from partial insert, search index empty",
    CoverageTokens: []string{"ON CONFLICT", "idempotent", "silently no-op", "partial insert", "execOnce stable", "touch source file"},
    Applies: func(p, host, role) bool { return hasManagedService(p, "search") && hasManagedService(p, "db") && role == "api" },
},
```

### B.6 Frontend (SPA / SSR)

```go
{
    ID: "frontend-build-time-env-baking",
    Description: "Vite/Webpack bake env vars at build time (not runtime) — VITE_API_URL set in run.envVariables is invisible to the bundled code; must be in build.envVariables",
    CoverageTokens: []string{"build time", "baked", "build.envVariables", "import.meta.env", "BUILD_API_URL", "VITE_", "NEXT_PUBLIC_"},
    Applies: func(p, host, role) bool { return role == "app" && usesBundler(p) },
},
{
    ID: "frontend-cors-credentials-origin",
    Description: "CORS origin must be explicit (not wildcard) when credentials: true — browser rejects wildcard + credentials combination",
    CoverageTokens: []string{"CORS", "Access-Control-Allow-Origin", "credentials", "preflight", "origin wildcard", "DEV_APP_URL"},
    Applies: func(p, host, role) bool { return role == "app" && hasDualRuntime(p) },
},
{
    ID: "frontend-spa-fallback-api-return-200-html",
    Description: "Static nginx SPA fallback returns 200+text/html for unknown paths; a missing build-time API URL causes /api/* to hit SPA and return HTML, breaking JSON.parse silently",
    CoverageTokens: []string{"200", "text/html", "SPA fallback", "SyntaxError", "Unexpected token", "content-type", "api guard"},
    Applies: func(p, host, role) bool { return role == "app" && hasStaticProd(p) },
},
```

### B.7 Dev-server / runtime generic

```go
{
    ID: "runtime-http-port-httpSupport-required",
    Description: "Non-HTTP listener on a port won't get subdomain access; httpSupport: true required to register at L7 edge",
    CoverageTokens: []string{"httpSupport", "serviceStackIsNotHttp", "subdomain", "L7", "not an HTTP port"},
    Applies: func(p, host, role) bool { return hasHTTPPort(p, host) },
},
{
    ID: "runtime-sshfs-chokidar-polling",
    Description: "SSHFS mount doesn't surface inotify events; file watchers need polling mode",
    CoverageTokens: []string{"SSHFS", "chokidar", "polling", "usePolling", "inotify"},
    Applies: func(p, host, role) bool { return isDevServerRuntime(p, host) },
},
{
    ID: "worker-no-readiness-check",
    Description: "Headless worker with no HTTP port must NOT set readinessCheck/healthCheck — validator rejects with serviceStackIsNotHttp",
    CoverageTokens: []string{"headless", "no HTTP", "no readinessCheck", "no healthCheck", "serviceStackIsNotHttp"},
    Applies: func(p, host, role) bool { return role == "worker" && !hasHTTPPort(p, host) },
},
```

### B.8 Projection rules

For any plan:

- **API codebase** typically gets: B.1 × 3 + B.2 × 2 + B.3 × 1 (if broker in plan but role=api) + B.4 × 2 + B.5 × 2 + B.7 × 2 = ~12 applicable candidates
- **Frontend codebase** typically gets: B.6 × 3 + B.7 × 2 (httpSupport + dev-server polling) = ~5 candidates
- **Worker codebase** typically gets: B.1 × 2 + B.3 × 4 + B.7 × 2 (worker-no-readiness, sshfs) = ~8 candidates

Floor of `ceil(0.5 × applicable)`:
- API: 6 of 12
- Frontend: 3 of 5
- Worker: 4 of 8

These floors align with v7 gold-standard gotcha counts (which had heavy invariant coverage) and give reasonable headroom for codebase-specific gotchas.

### B.9 Corpus governance

- Adding a candidate requires a version-log entry where the intersection surfaced (prevents speculative additions).
- Removing a candidate requires ≥3 recipe runs with zero hits on that candidate (prevents premature removal).
- Every PR that modifies the corpus must update this appendix with the before/after rationale.
- Candidates that produce false-positive rates >10% in 3 consecutive runs are demoted to informational or refactored.

---

## Closing: the 98% path

v8.82's job: take v22's B to v23's A, via five fixes that each address a specific structural asymmetry the v22 post-mortem surfaced. The design principle throughout is **positive-signal pressure over negative-filter penalty**:

- §4.1 invariant-coverage **rewards** platform-knowledge inclusion; doesn't punish incident gotchas
- §4.2 zerops.yaml depth **rewards** reasoning-anchored comments; IG #1 inherits by construction
- §4.3 overview topic **rewards** coherent authorship; no check, just teaching
- §4.4 container-ops nudge **informs**, doesn't block
- §4.5 IG causal-anchor **extends** proven rubric symmetry; mirrors gotcha check shape

No session-log feedback. No framework hardcoding. No new subagent dispatch gates. No Goodhart traps.

The remaining 2% gap after v8.82 is honest — it's model variance, corner-case intersection surprises, and the inherent irreducible uncertainty of agent stochasticity within a gate-accepting envelope. v8.83 post-mortem territory.

v8.82 is designed to be the last post-mortem that matters at current fidelity. After it lands and v23 validates at A, the next measurable gains come from *different* fidelity dimensions (multi-framework showcase runs, new service categories, scaling beyond 3 codebases) — not from continuing to extract structural asymmetries from the same NestJS stack.

That's the 98% path.
