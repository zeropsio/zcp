# Run-39 validation — first prod dogfood after v9.80.0

**Run dir:** `/Users/fxck/www/zcp/docs/zcprecipator3/runs/39/`
**Release under test:** v9.80.0 (7 fixes closing run-38 residuals)
**Baseline:** [`plans/run-38-validation.md`](run-38-validation.md)
**Spec:** [`plans/v9.80.0-residual-fixpack-spec.md`](v9.80.0-residual-fixpack-spec.md)

---

## Headline

**production-ready with one substrate-internalization residual for v9.80.1.**

All 7 v9.80.0 engine fixes landed (Fix 4 banner reconciliation, Fix 5 RF1/PD1/Understand absence, Fix 7 KB H3 — all CLEAN across deliverables). All 8 mechanical counters at-or-above run-38 floor; counter 7 (KB sibling shape) shows structural improvement from "3 distinct H2/H3/headerless" to "3× unified H3 problem-statement". Tool/SSH fail counts within ±20% (run-39 total 32 vs run-38 total 37, net −5). Content quality on revised patterns reads at-bar or above (Fix 1 lead-voice clean; Fix 2 TY5 block identical across all 6 tiers; Fix 4 banner exact byte-match on tier-2..5).

One **systematic substrate failure**: Fix 2 gate (`missing-priority-justification-block`) fired across **7 distinct env-content phase-close retries** in one sub-agent (spec threshold: >3 = substrate didn't internalize). The engine teeth held — the deliverable is clean — but the gate did the substrate's job. Substrate teaching needs to be tightened in v9.80.1 so the env-content agent authors the TY5 block on first pass.

KB dashboard rendering still requires user-side browser verification — I have no signal either way (see Job 2 §Fix 7+7b). Fix 7b carries a **partial engine landing**: the invariant holds for `intro` and `knowledge-base` markers but the integration-guide marker bypasses Fix 7b via a separate code path. Dashboard-affecting surface (KB) is correct; IG marker shape doesn't affect the dashboard render.

---

## Pre-flight result

v9.80.0 binary correctly stamped. Fix 3 gate held (no engine-version-mismatch fires).

| Check | Expected | Actual | Verdict |
|---|---|---|---|
| `plan.json` engineVersion | `v9.80.0` | `"engineVersion": "v9.80.0"` (pretty-printed) | ✓ |
| Tier-0 banner | `# NestJS Showcase — AI Agent Environment` | exact match (capital A on Agent — Fix 4) | ✓ |
| Tier-1 banner | `# NestJS Showcase — Remote (CDE) Environment` | exact match (Fix 4 partial revert restored canonical) | ✓ |
| Apps-repo H2s (RF1/PD1/Understand) | 0 across apidev/appdev/workerdev | 0/0/0 | ✓ (Fix 5) |
| KB extract opens with H3 | first non-blank line is `### …` across all 3 codebases | all 3 codebases match | ✓ (Fix 7) |
| Blank line after every EXTRACT_START marker | yes across intro / IG / KB | intro ✓, KB ✓, **IG ✗** | partial (Fix 7b) |

**Fix 7b partial:** the `integration-guide` marker is followed immediately by `### 1. Adding zerops.yaml` (no blank line) across all three codebases. Root cause: [`internal/recipe/assemble.go:285-316`](../internal/recipe/assemble.go#L285-L316) `injectIGItem1` writes its own marker block (`start + "\n"` at line 308) AFTER `substituteFragmentMarkers` runs, bypassing the Fix 7b emission at [`assemble.go:672`](../internal/recipe/assemble.go#L672). Fix 7b's unit test [`TestSubstituteFragmentMarkers_BlankLineAfterStart`](../internal/recipe/assemble_test.go#L435) doesn't cover this path. **Not gate-failing** for the dashboard hypothesis (KB renders, IG isn't extracted to the recipe dashboard page) — but `injectIGItem1` should also emit `\n\n` for consistency. **Recommended for v9.80.1 cleanup.**

Pre-flight VERDICT: pass — proceed with full validation.

---

## Regression scan

### Tool-call fail counts

| Metric | run-38 | run-39 | Δ | Verdict |
|---|---|---|---|---|
| Main-session `is_error:true` | 4 | 2 | −2 | ✓ improved |
| Sub-agent `is_error:true` total | 33 | 30 | −3 | ✓ within ±20% (−9%) |
| **All sub+main total** | **37** | **32** | **−5** | ✓ improved |
| `ok:false` in MCP responses | 0 | 0 | 0 | ✓ at-bar |
| SSH-failure tokens (ssh:/Permission denied/exit-code>=1) | 1 | 1 | 0 | ✓ at-bar |

### Per-sub-agent breakdown (where the layer-shift happened)

| Sub-agent kind | run-38 | run-39 | Δ | Read |
|---|---|---|---|---|
| `env-content` | 1 | **13** | **+1200%** | Fix 2 gate-loop (see below) — substrate internalization failure |
| `codebase-content-api` | 6 | 0 | −100% | Fix 5 + Fix 7 substrate landed cleanly |
| `codebase-content-app` | 4 | 0 | −100% | same |
| `codebase-content-worker` | 2 | 1 | −50% | same |
| `scaffold-api` | 5 | 3 | −40% | drift |
| `scaffold-worker` | 3 | 5 | +67% | small N, within drift |
| `scaffold-app` | 4 | 3 | −25% | drift |
| `features-backend` | 4 | 3 | −25% | drift |
| `features-frontend` | 2 | 0 | −100% | drift |
| `refinement` | 2 | 2 | 0 | at-bar |

**Net total stayed within ±20%** but the layer-shift is substantial: codebase-content sub-agents collectively dropped from 12 → 1 errors (Fix 5/Fix 7 substrate landed cleanly), while env-content single-handedly spiked from 1 → 13 driven by Fix 2 substrate failure. **The engine teeth held; the deliverable is clean; but the new gates are doing substrate work.**

### Gate-fire counts (strict — distinct tool_result responses carrying the violation)

| Gate | Threshold | run-39 | Verdict |
|---|---|---|---|
| `engine-version-mismatch` (Fix 3) | MUST be 0 | 0 | ✓ |
| `engine-version-not-stamped` (Fix 3) | MUST be 0 | 0 | ✓ |
| `managed-service-comment-tradeoff-lead` (Fix 1) | 0–2 | 0 | ✓ substrate worked |
| `missing-priority-justification-block` (Fix 2) | 0–2 | **7** | ✗ **substrate failure** |
| `object-storage-missing-priority` (Fix 6) | 0–2 | 1 | ✓ at-boundary |
| `kb-fragment-not-h3-rooted` (Fix 7) | 0–2 | 0 | ✓ substrate worked |
| Old `apps-repo-has-rf1` / `non-canonical-apps-repo-has-rf1` (run-38: 3) | 0 | 0 | ✓ |
| Old `apps-repo-has-pd1` / `non-canonical-apps-repo-has-pd1` (run-38: 1) | 0 | 0 | ✓ |
| Old `canonical-apps-repo-missing-rf1` (run-38: 1) | 0 | 0 | ✓ |

Methodology: count of distinct tool_result messages (not raw string occurrences) that carry the gate's violation code via escaped JSON. Codex confirmed gate-zero counts; the positive counts (`7`, `1`) measure **distinct phase-close retries that the gate refused**, not aggregate violation entries across multi-tier batched failures (which produces inflated counts of 33–70 depending on grep methodology).

All zero-count gates are independently confirmed; Fix 2 = 7 retries is the actionable signal.

---

## Engine fix verification matrix

| # | Fix | Surface(s) | Expected | Actual | Verdict |
|---|---|---|---|---|---|
| 1 | TY2 BAD lead refusal on db comments | tier 0..5 db comment lead voice | role-first lead; tradeoff as supporting | tier-0..2 = role-relative; tier-3+4 = `"Single-instance Postgres — snapshots-only durability..."` (role-first, tradeoff supporting); tier-5 = HA framing | ✓ |
| 2 | TY5 priority-justification block | tier 0..5 import.yaml | block present before first `priority: 10` | `# Set higher priority for databases and storages, because the app depends on those services.` verbatim across all 6 tiers | ✓ (with substrate-failure note) |
| 3 | engine-version stamp | `plan.EngineVersion = v9.80.0` | every phase-close passes gate | 0 fires | ✓ |
| 4 | tier-label canonical (AI Agent capital A; Remote (CDE) preserved) | tier 0..5 README L1 | exact engine emit | all 6 tier banners exact | ✓ |
| 5 | RF1/PD1/Understand absence in apps-repos | apidev/appdev/workerdev README structure | 0 of these H2s, no disguised duplication | 0/0/0; H2 structure = `## Deploy to Zerops` → `## Integration Guide` only | ✓ |
| 6 | object-storage priority parity | tier 0..5 storage service | `priority: 10` on every tier | 6/6 tiers have `priority: 10` on `type: object-storage` | ✓ |
| 7 | KB fragment opens with H3 | apidev/appdev/workerdev KB extract | first non-blank line is `### ` heading | 3/3 codebases — problem-statement form | ✓ |
| 7b | Engine emits `\n\n` after EXTRACT_START marker | intro / integration-guide / knowledge-base markers across 3 codebases | blank line after every START marker | intro ✓, KB ✓, **IG ✗** (3/3 codebases) | partial — see pre-flight |

7 of 7 functional fixes landed. Fix 7b has a code-hygiene gap on the IG path that doesn't affect the spec's stated dashboard-render hypothesis (KB is what renders, KB is correct).

---

## Content quality posture (porter-empathy read)

### Fix 1 — db comment lead voice

| Tier | Lead clause | Voice |
|---|---|---|
| 0 (AI Agent) | `Deploy low-resource PostgreSQL database, used by api + worker to store items + job state.` | role + relationship — TY1 ✓ |
| 1 (Remote CDE) | `Same single-instance Postgres as tier 0 — the human-porter CDE workspace is replaceable data, snapshots cover restore.` | role-relative, tradeoff supporting ✓ |
| 2 (Local) | `Same single-instance Postgres as the dev tiers — minimum RAM, snapshots-only recovery, sized for sanity-check traffic.` | role-relative, tradeoff supporting ✓ |
| 3 (Stage) | `Single-instance Postgres — snapshots-only durability at this rehearsal tier; restoring still means downtime.` | role-first, tradeoff as supporting clause (the explicit spec carve-out) ✓ |
| 4 (Small Production) | `Single-instance Postgres — snapshots-only durability, the small-prod tradeoff.` | role-first, tradeoff supporting ✓ |
| 5 (HA Production) | `Postgres in HA mode — clustered nodes survive single-node loss without downtime.` | HA framing, no tradeoff ✓ |

The run-38 BAD pattern `"restoring from snapshot... tolerate a brief restart window"` is GONE across all tiers. Tier-3+4 still mention durability/snapshots, but as the SECOND clause behind the role-first lead. Per spec: "the right voice keeps the tradeoff as a second clause" — exactly the shape achieved. Quality: AT-BAR or above.

### Fix 2 — TY5 priority justification

Identical wording across all 6 tiers:

```
# Set higher priority for databases and storages, because the
# app depends on those services.
```

Placed immediately before the first `priority: 10` service in each tier yaml. Same-author-across-tiers shape — clean. Wording succinctly explains the WHY (boot-order dependency: app depends on those services). Per-service comments following the block elaborate (`priority: 10 keeps Postgres up before api initCommands run migrate + seed`).

Quality: AT-BAR. **However the 7 gate fires in env-content show the agent didn't internalize the canonical form on first pass — it took multiple gate-refusals before the deliverable converged.** This is a substrate teaching gap, not a content quality gap.

### Fix 4 — tier-label canonical

L1 banner across all 6 tiers exact:

```
# NestJS Showcase — AI Agent Environment        (tier 0; capital A)
# NestJS Showcase — Remote (CDE) Environment    (tier 1; parenthetical preserved)
# NestJS Showcase — Local Environment           (tier 2)
# NestJS Showcase — Stage Environment           (tier 3)
# NestJS Showcase — Small Production Environment (tier 4)
# NestJS Showcase — Highly-available Production Environment (tier 5)
```

L2 sentence form: `"This is an AI agent environment for [recipe-link] recipe on Zerops"` (lowercase "agent" in sentence form per `tierLabelLower` algorithm — correct).

Quality: AT-BAR. Engine-emitted, no agent variance.

### Fix 5 — RF1/PD1/Understand absence

apps-repo H2 structure for all three codebases:

```
# Zerops x NestJS Showcase {API|Frontend|Worker}
intro
cover image
## Deploy to Zerops
deploy button
## Integration Guide
{IG items}
KB extract
```

NO disguised duplication anywhere. No `## Recipe Overview` or `### Production migration paths` or similar shifted headings carrying forbidden content. The substrate-teaching update plus the widened forbid-gate both landed cleanly across deliverables. Codebase-content sub-agents dropped from 12 → 1 errors (Fix 5/Fix 7 together).

Quality: AT-BAR or above (cleanest section ordering of any run since run-32).

### Fix 6 — object-storage priority parity

All 6 tiers have `priority: 10` on `type: object-storage`. TY5 justification block uses plural "storages" which encompasses object-storage (semantically correct — the agent chose a more general formulation than the substrate's strict `databases and object-storage` recommendation). 1 gate fire on first env-content attempt; agent corrected.

Quality: AT-BAR. Substrate teaching landed cleanly (only 1 gate fire across the entire run).

### Fix 7 + Fix 7b — KB H3 + blank-line

Per-codebase KB extracts:
- apidev: 7 entries — NATS auth, object-storage 403, CORS hidden headers, CORS allow-list race, Valkey aliases, Meilisearch masterKey, parent-recipe reference
- appdev: 4 entries — Vite allowedHosts, X-Cache null, upload-card duality, queue counter polling
- workerdev: 4 entries — NATS queue option, drain on SIGTERM, Authorization Violation, missing env vars

All three KB extracts:
- Open with `### ` problem-statement heading ✓
- Use paragraph-shape inside each entry (not flat bullets, not H2) ✓
- Voice consistent: problem-statement → diagnostic paragraph → optional code block → maintenance hint
- Per-recipe specificity is appropriate; no generic boilerplate

**Sibling shape consistency** (Counter 7): run-38 had 3 distinct shapes (apidev=headerless, appdev=`## Tips and Others` H2, workerdev=`## Tips and Other Considerations` H2). Run-39 has 3× H3 problem-statement form — **structural improvement, not just statistical**.

**Dashboard render verification: I have no signal either way.** Per `feedback_codex_validation.md` this is the only finding that needs human dashboard observation; flagging explicitly. The H3 + KB-marker-blank-line hypothesis is correctly embodied in the deliverable; whether the recipe page at `app.zerops.io/recipes/nestjs-showcase` actually renders the KB section is unverifiable from here.

Quality: AT-BAR. **Hypothesis verification deferred to user.**

---

## Mechanical counters

| # | Counter | Run-37 | Run-38 | **Run-39** | Verdict |
|---|---|---|---|---|---|
| 1 | Cross-codebase env coherence (shared-alias mismatches) | 0 | 0 | **0** | ✓ at-bar |
| 2 | English-cased slug leakage in body prose | 0 | 0/3 | **0/3** (title-line "NestJS Showcase" is canonical, not leakage) | ✓ at-bar |
| 3 | Cross-framework verb count (legitimate-only) | n/a | 5 hits legitimate | **2 hits legitimate** (Express adapter mentions in apidev IG) | ✓ at-bar (drift) |
| 4 | Sharpened voice leak (Laravel/Jetstream/Rails outside .briefs/) | 0 | 0 | **0** | ✓ at-bar |
| 5 | Forbidden-token contamination (11 tokens) | 0/N | 0/11 | **0/11** | ✓ at-bar |
| 6 | tier_decision Why-fill rate | 100% | 100% | **100%** (10/10) | ✓ at-bar |
| 7 | KB-header consistency across siblings | 3 distinct (mixed H2/H3/H3) | 3 distinct (headerless/H2/H2) | **3 unified H3 problem-statement** | ✓ structural improvement |
| 8 | yaml comment density per codebase | apidev 46.4% / appdev 59.5% / workerdev 47.1% | apidev 51.0% / appdev 56.9% / workerdev 55.6% | **apidev 48.5% / appdev 63.1% / workerdev 54.1%** | apidev −2.5pp; appdev +6.2pp; workerdev −1.5pp — all within ±20% drift floor ✓ |

Counter 7 is the headline: Fix 7 closed the run-37/38 sibling-shape divergence (Finding H from placement audit) **structurally**. All other counters at-or-above floor.

---

## Side-by-side vs jetstream golden (Fix-5-aware)

| Surface | Verdict | Note |
|---|---|---|
| Tier banner L1 (tiers 2..5) | **at-bar** | engine emits byte-for-byte match |
| Tier banner L1 (tier 0) | **at-bar** | engine now matches jetstream `AI Agent` (capital A) per Fix 4 |
| Tier banner L1 (tier 1) | **engine canonical, golden divergent** | engine `Remote (CDE) Environment` (Fix 4 partial revert); jetstream hand-strips to `Remote Environment`. Engine is the source of truth. |
| Tier banner L2 sentence form | **at-bar** | engine-generated, matches algorithm |
| Tier yaml head comment | **above-bar** | run-39 head comments are substantively richer than jetstream's minimal "Small production environment offers..." line — porter-empathy higher |
| Per-service comment voice (db, cache, broker, search, storage) | **at-bar** | role-first imperative leads with relationship context across all tiers |
| Object-storage `priority: 10` placement | **at-bar** | both run-39 and jetstream-golden have it on every tier |
| Root porter-meta line | **at-bar** | engine-hardcoded since v9.78.0; matches `# {Recipe} Recipe` shape |
| Apps-repo H2 structure (RF1/PD1/Understand) | **engine-correct, golden-incorrect** | run-39 omits these per Fix 5; jetstream-golden still carries them at lines 223/228/241 (golden below-bar post-v9.80.0) |
| KB extract shape | **above-bar** | run-39 uses H3 problem-statement form across all three codebases; jetstream-golden uses mixed shapes |
| IG section structure | **at-bar** | `## Integration Guide` H2 → numbered H3 items, engine-emitted item #1 yaml-block |

Net: run-39 either matches or exceeds jetstream golden on every comparable surface. The two engine-vs-golden divergences (tier-1 banner + Fix 5 absence) are **intentional engine canonicality**, not run-39 below-bar.

---

## IG count tracking (NEW-1 from run-38)

| Codebase | run-37 | run-38 | run-39 | Δ vs run-38 |
|---|---|---|---|---|
| apidev | 5 | 5 | 5 | 0 |
| appdev | 5 | 5 | 4 | −1 (within drift) |
| workerdev | 6 | 4 | 4 | 0 (NEW-1 persists, not regressed further) |

The run-38 NEW-1 (workerdev IG count 6→4) holds at 4. appdev dropped 1 item; both quality and substrate-de-emphasis hypotheses still plausible. Within ±20% stochastic floor — track-only, not blocking.

---

## Top 5 surprises

1. **Codebase-content sub-agents went from 12 errors → 1.** Fix 5 (RF1/PD1/Understand widened forbid-gate) and Fix 7 (KB H3 requirement) both translated into clean codebase-content runs — the substrate teaching landed without the agent needing gate-corrections. Best codebase-content sub-agent performance of any run since run-32.

2. **Env-content sub-agent went from 1 error → 13.** Driven entirely by Fix 2 (`missing-priority-justification-block`) — 7 distinct phase-close retries before the agent authored the canonical TY5 block. The deliverable is correct; the substrate didn't internalize the new rule. Engine teeth shouldered substrate's job. **v9.80.1 substrate tightening recommended.**

3. **Fix 7b is partial — engine landing didn't fully cover the IG marker.** Spec wrote a unit test ([`TestSubstituteFragmentMarkers_BlankLineAfterStart`](../internal/recipe/assemble_test.go#L435)) that covers `substituteFragmentMarkers` but not [`injectIGItem1`](../internal/recipe/assemble.go#L285). All three codebases' IG markers lack the blank line. Not gate-failing for the dashboard hypothesis (KB renders, IG isn't dashboard-extracted), but a code-hygiene gap. Cleanup pin candidate.

4. **KB sibling shape unified structurally.** Run-37 + run-38 each had 3 distinct KB heading shapes across siblings; run-39 has 3× H3 problem-statement form, all with consistent paragraph-shape body. **Finding H (placement-audit residual) is structurally closed**, not just statistically.

5. **No dashboard render signal.** The whole v9.80.0 KB-rendering hypothesis (Fix 7 + Fix 7b) is verifiable only on the live dashboard at `app.zerops.io/recipes/nestjs-showcase`. From repo-side I can only confirm the invariants land in the deliverable. **User browser verification required to close the iteration.**

---

## Codex validation

Spawned [codex:codex-rescue](file:///Users/fxck/www/zcp/.claude/plugins/codex/) on findings F1–F10. Verdicts:

| Finding | Verdict | Note |
|---|---|---|
| F1 engineVersion stamp | CONFIRMED | pretty-printed JSON; grep pattern needs space tolerance |
| F2 Fix 4 banners | CONFIRMED | tier-0 capital A, tier-1 parenthetical preserved |
| F3 Fix 5 absence | CONFIRMED | 0 hits across all 3 apps-repo READMEs |
| F4 Fix 7 KB H3 | CONFIRMED (with phrasing correction) | "first non-blank line is H3" is the strict property — there's always a blank line between marker and H3 (Fix 7b's intent). Analyst's "opens with H3" was loose but the property holds. |
| F5 Fix 7b partial | CONFIRMED | intro ✓, KB ✓, IG ✗ across all 3 codebases |
| F6 gate-fire counts | METHODOLOGY CORRECTED | Codex's raw-grep counts (70/5/33/1) measured per-tier violation-entry occurrences; my retry-count methodology (`7/1` distinct tool_result responses) is the actionable signal. Re-derived using escaped-JSON `\"code\":\"...\"` extraction. |
| F7 per-agent is_error | CONFIRMED | run-39 total 32, run-38 total 37, env-content 13 vs 1, codebase-content 1 vs 12 |
| F8 8 mechanical counters | CONFIRMED | all 8 at-or-above floor |
| F9 Fix 1 lead voice | CONFIRMED | run-38 BAD pattern `"restoring from snapshot..."` GONE on tier-3+4 |
| F10 Fix 2 TY5 block | CONFIRMED | identical across all 6 tiers, placed before first priority:10 |

No additional regressions surfaced by codex.

---

## Recommended next step

**Capture run-39 as new sim baseline; ship v9.80.0 deliverable; queue v9.80.1 substrate tightening.**

Conditions met:
- All 7 v9.80.0 engine fixes landed (7/7 functional; Fix 7b partial on non-dashboard path)
- No regression in tool/SSH/gate fail counts (net −5 total errors)
- Content quality on revised patterns at-bar or above run-38
- Counter 7 closed Finding H structurally

Condition NOT met:
- Fix 2 substrate didn't internalize (7 gate fires > 3 threshold) — engine teeth held but at the cost of layer-shift in env-content phase. **The deliverable is correct.** **The substrate is not yet self-sufficient.**

Condition deferred:
- KB dashboard render verification — needs user browser observation at `app.zerops.io/recipes/nestjs-showcase`. If KB renders → v9.80.0 closes iteration. If KB still doesn't render → hypothesis wrong; investigate dashboard parser before proposing v9.80.1.

### v9.80.1 candidate scope (substrate-only, no new gates needed)

1. **RES-3 (new) — Fix 2 substrate internalization.** Sharpen env-content brief to author the TY5 priority-justification block on first pass. Concrete change: the brief at `env-content-phase/part-1-phase-entry.md` should include a worked example (canonical block + placement: immediately before first `priority: 10`) and the gate's canonical wording as a verbatim teaching artifact, not a generated-from-rule hint. Goal: 0 gate fires in run-40.

2. **RES-4 (new — code hygiene) — Fix 7b IG-marker coverage.** Update `injectIGItem1` to also emit `\n\n` after the START marker (currently single `\n` at [`assemble.go:308`](../internal/recipe/assemble.go#L308)); extend `TestSubstituteFragmentMarkers_BlankLineAfterStart` to exercise the IG-injection path. Not user-visible but closes Fix 7b's stated invariant across all extract markers.

### Defer

- **NEW-1 IG count drift** (appdev 5→4, workerdev steady at 4). Within ±20% drift floor; revisit in run-40+ if it moves.
- **Tier-1 banner engine-vs-golden divergence.** Engine canonical `Remote (CDE)` is correct per Fix 4 spec; jetstream-golden is hand-stripped. If golden needs to be re-aligned, that's a jetstream-side edit, not zcp-side.

### Do NOT

- **Add a new engine gate for Fix 2 reinforcement.** The gate already exists and is doing exactly what it's supposed to — refusing bad deliverables. Adding a parallel gate doesn't solve substrate non-internalization; sharper substrate teaching does.
- **Propose v9.80.1 KB-render changes** until user confirms whether KB renders on dashboard. If the hypothesis is wrong, the next round of changes needs dashboard-parser evidence, not more engine teeth.
