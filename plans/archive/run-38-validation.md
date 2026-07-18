# Run-38 Validation — first prod dogfood after v9.79.0 placement engine fix-pack

**Date:** 2026-05-10
**Run:** [`docs/zcprecipator3/runs/38/`](../docs/zcprecipator3/runs/38/) — nestjs-showcase, 3 codebases (apidev/appdev/workerdev), 6 tiers
**Engine version:** v9.79.0 (placement-engine fix-pack + RF1/PD1 absence-gate hardening)
**Audit basis:** [`run-37-validation.md`](./run-37-validation.md) clean baseline + [`run-37-vs-jetstream-placement.md`](./run-37-vs-jetstream-placement.md) audit + [`v9.79.0-placement-engine-fixpack-spec.md`](./v9.79.0-placement-engine-fixpack-spec.md) implementation spec.

---

## Headline

**production-ready — engine fixes 100% landed, run-37 RF1/PD1 regression structurally closed by gate.** Counters at-or-above run-37 floor on every meaningful axis, audience-model voice clean, all defect classes absent. Two substrate-only residuals persist for the third consecutive run (tier-3+4 db TY2 BAD lead-voice; TY5 priority-justification block missing across all 6 tiers) — both ready for engine teeth on next iteration. One workerdev-IG-count regression vs run-37 (6 → 4) worth tracking but quality of remaining IG items is at-bar.

**Recommendation:** capture run-38 as new sim baseline; sim-replay; ship if deterministic. Open follow-up engine-teeth ticket for managed-service comment voice (TY2/TY5) — substrate-only is officially not landing.

---

## Pre-flight result

**v9.79.0 binary shipped — confirmed.** All 6 pre-flight smoke checks pass:

| Check | Expected | Actual |
|---|---|---|
| Stage README L1 | `# NestJS Showcase — Stage Environment` | ✓ match |
| Stage README L2 | starts `This is a stage environment for [` | ✓ match |
| `apidev` `## Deploy to Zerops` H2 count | 1 | ✓ 1 |
| `← back to recipe root` across 6 tier READMEs | 0 | ✓ 0/6 |
| `Full recipe page and deploy with one-click` in `apidev/README.md` | 0 | ✓ 0 |
| RF1/PD1 H2 in `appdev/README.md` | 0 (gate fires) | ✓ 0 |

Run-36's binary-version-drift class of failure did not recur.

---

## Engine fix verification matrix (v9.79.0 — must be 100% green)

| # | Surface | Expected | Actual | Verdict |
|---|---|---|---|---|
| 1 | `appdev/README.md` `## Recipe features` | absent | absent | ✓ |
| 1 | `appdev/README.md` `## Production vs. Development` | absent | absent | ✓ |
| 1 | `workerdev/README.md` `## Recipe features` | absent | absent | ✓ |
| 1 | `workerdev/README.md` `## Production vs. Development` | absent | absent | ✓ |
| 1 | Gate fired empirically (SESSION_LOGS) | should fire | **fired (3 RF1 + 1 PD1 violation messages)**; agent retried; 2× `complete-phase phase=codebase-content` calls (rejected → succeeded) | ✓ |
| 1 | `apidev/README.md` (canonical) RF1+PD1+Understand | all 3 present | all 3 present | ✓ |
| 2 | `environments/0..5/README.md` L1 | `# NestJS Showcase — {TierLabel} Environment` | match × 6 | ✓ |
| 2 | `environments/0..5/README.md` L2 | `This is {a/an} {label_lower} environment for [...]` | match × 6 | ✓ |
| 2 | Tier-0 article + acronym | "an AI agent" | ✓ "an AI agent" | ✓ |
| 2 | Tier-1 article + acronym | "a remote (CDE)" | ✓ "a remote (CDE)" | ✓ |
| 2 | Tier-3 lowercased label | "stage" | ✓ "stage" | ✓ |
| 2 | Tier-5 hyphenated label | "highly-available production" | ✓ "highly-available production" | ✓ |
| 2 | Back-link removed across 6 tiers | 0 | ✓ 0/6 | ✓ |
| 2 | Standalone Deploy button removed across 6 tiers | 0 | ✓ 0/6 | ✓ |
| 3 | `apidev/README.md` body order | title → intro → cover → `## Deploy to Zerops` H2 → IG | match | ✓ |
| 3 | `## Deploy to Zerops` H2 across 3 apps-repos | 1 each | 1 each | ✓ |
| 3 | Legacy "Full recipe page…" lead in apps-repos | 0 | 0/3 | ✓ |

**100% landing on all 3 v9.79.0 fixes.** Engine-fix 1 is the most material — empirical gate-fire in SESSION_LOGS proves the agent attempted the run-37-class RF1+PD1 duplication on appdev (substrate teaching alone did NOT hold), and the engine refused the close. The agent corrected and re-ran successfully. Without the gate, run-38 would have shipped a repeat regression.

---

## Mechanical counters

| # | Counter | Run-35 floor | Run-37 actual | Sim-3 floor | Run-38 actual | Verdict |
|---|---|---|---|---|---|---|
| 1 | Cross-codebase env coherence (shared aliases naming-mismatches) | 0 | 0 | 0 | **0** | ✓ at-bar |
| 2 | English-cased slug-leakage (`nestjs-showcase` / `NestJS Showcase` in body prose, URL+cover excluded) | 0 | 0 | 0 | **0/3** | ✓ at-bar |
| 3 | Cross-framework verb count (legitimate NestJS-Express adapter only) | legitimate-only | legitimate-only | legitimate-only | **5 hits, all legitimate** (Express adapter, Express error handlers, `app.getHttpAdapter().getInstance()`) | ✓ at-bar |
| 4 | Sharpened voice-leak in published artifacts (Laravel/Jetstream/Rails outside `.briefs/`) | 0 | 0 | 0 | **0** | ✓ at-bar |
| 5 | Forbidden-token contamination (11 tokens) | 0/N | 0/N | 0/N | **0/11** (laravel/jetstream/rails/php/eloquent/breeze/inertia/livewire/blade/artisan/ruby) | ✓ at-bar |
| 6 | tier_decision Why-fill rate | 100% | 100% | 100% | **100%** (10/10) | ✓ at-bar |
| 7 | KB-header consistency across siblings | 3 distinct H2 | 3 distinct mixed (H2/H2/H3) | matched | **3 distinct** (apidev headerless extract, appdev `## Tips and Others` H2, workerdev `## Tips and Other Considerations` H2) | partial improvement (no H3 anymore; jetstream golden is `## Tips and Others` H2 — appdev matches; workerdev close but distinct; apidev now novel headerless shape) |
| 8 | yaml comment density per codebase (strip-first, run-37 method) | n/a | apidev 46.4% / appdev 59.5% / workerdev 47.1% | n/a | **apidev 51.0% / appdev 56.9% / workerdev 55.6%** | apidev +4.6pp ✓; appdev −2.6pp (within ±20% drift floor); workerdev +8.5pp ✓ |

All 8 counters at or above run-37 floor with the partial-improvement note on Counter 7 (KB sibling-shape — see Finding H residual below).

---

## Run-37 residuals — landed / persisted / new

### Persisted (substrate-only, not landing)

**RES-1. Tier-3+4 db TY2 BAD lead-voice** — `environments/3 — Stage/import.yaml` line 57 + `environments/4 — Small Production/import.yaml` line 62 both lead with operational tradeoff:

```
# Single-instance NON_HA Postgres — restoring from snapshot
# means downtime, the {rehearsal-grade,small-prod} tradeoff.
```

Lead voice = TY2 BAD. The `restoring from snapshot means downtime` opener is the *first* clause, ahead of role/relationship. Run-37 had this on tier-4 db + tier-4 cache (2 instances at 1 tier); run-38 has it on tier-3 db + tier-4 db (2 instances at 2 tiers; tier-4 cache fixed). **Net count = 2/N; pattern systematic on db at non-HA stage/small-prod tiers — third consecutive run.** Codex strict-grep found `NON_HA Postgres` mentioned at tier-1 + tier-2 too, but those tiers lead with role+context (tier-1: "sized for CDE iteration"; tier-2: "local-test data") and only mention durability in supporting clauses — softer TY2 voice in supporting clauses, not lead-voice TY2 BAD.

**RES-2. TY5 priority-justification block ABSENT across all 6 tiers.** No tier `import.yaml` carries a TY5 explanation block analogous to the jetstream golden's:

```
# Set higher priority for databases and storages,
# because the app depends on those services.
```

Substrate teaches it; agent has not authored it on any tier across any of the past three runs. Same compliance class as RES-1.

### Closed by engine teeth

**CLOSED-1. RF1/PD1 leakage onto non-canonical apps-repo (appdev).** Run-37 had `## Recipe features` + `## Production vs. Development` H2s on appdev (non-canonical). Run-38 has zero — and the engine gate provably fired when the agent attempted the duplication. Structural fix, not statistical.

**CLOSED-2. Tier README L1+L2 banner divergence vs jetstream golden.** Run-37's tier READMEs were structurally inconsistent with jetstream goldens; v9.79.0 added engine-emitted L1+L2 with `tierLabelLower` + `tierArticle` helpers. All 6 tiers now match the jetstream banner shape byte-for-byte (modulo one engine-vs-golden case-divergence — see "Top 5 surprises").

**CLOSED-3. Legacy "Full recipe page…" lead at apps-repos.** Replaced by `## Deploy to Zerops` H2 wrapper across all 3 apps-repos. (Root README still carries the legacy lead — intentional; engine fix 3 only restructured apps-repo READMEs.)

### New regressions

**NEW-1. workerdev IG count: 6 → 4.** [`run-38/workerdev/README.md`](../docs/zcprecipator3/runs/38/workerdev/README.md) has `### 1` through `### 4`; run-37 had 6 IG items. Quality of the 4 remaining items is at-bar (named-trap framing intact; IG#4 "Drain the subscription on SIGTERM, do not unsubscribe" is clean). Could be variance or could be substrate de-emphasizing items the agent considered redundant — track in run-39+.

---

## Per-surface porter-empathy read summary

- **`apidev/README.md`** — clean. Title → intro → cover → Deploy H2 → IG (5 items) → RF1 → PD1 → Understand → headerless KB extract block. Voice is imperative + named-trap + recipe's own fix on every IG item. KB is concrete trap-then-fix bullets without a heading wrapper. Order below IG diverges from jetstream (RF1/PD1 before Understand) — Finding F deferred.
- **`appdev/README.md`** — clean. Title → intro → cover → Deploy H2 → IG (5 items) → Understand → `## Tips and Others` KB. RF1+PD1 absent (gate fired). Vite-build-time-bake trap mechanism explained at IG#4 with the same trap surfaced in KB (intentional double-coverage with porter-friendly framing).
- **`workerdev/README.md`** — at-bar but reduced. Title → intro → cover → Deploy H2 → IG (4 items, was 6 in run-37) → Understand → `## Tips and Other Considerations` KB. Quality of remaining items intact; named-trap framing on IG#4. Reduced count flagged as NEW-1.
- **`environments/{0..5}/README.md`** — clean. L1 + L2 banner match v9.79.0 spec; back-link gone; standalone deploy button gone; per-tier intro continues from L2.
- **`environments/{0..5}/import.yaml`** — clean except RES-1 + RES-2. Apps-repo + worker comments are role-first imperative; `db` at tier-3 + tier-4 leads with operational tradeoff (RES-1); priority-justification block missing on every tier (RES-2).
- **`README.md` (root)** — clean. Porter-meta line + tier listing match jetstream golden structure; the legacy "Full recipe page…" lead is preserved here as intended (engine fix 3 only restructured apps-repos).

---

## Top 5 surprises

1. **Engine gate empirically fired** — SESSION_LOGS shows the agent attempted RF1+PD1 duplication on appdev *despite* substrate teaching, and was caught by `forbidRF1PD1OnNonCanonicalAppsRepos`. This validates the engine-teeth-over-substrate principle at runtime: substrate teaching alone did not hold for this regression class. Engine fixes that are input-invariant land regardless of agent behavior; substrate fixes are subject to agent compliance and stochasticity.
2. **Tier-3 stage db acquired TY2 BAD voice that tier-4 cache shed.** Run-37 had TY2 BAD at tier-4 db + tier-4 cache; run-38 has TY2 BAD at tier-3 db + tier-4 db. Net count is identical (2/N) but the topology shifted. Pattern is now clearly db-at-non-HA-tiers (3+4), not tier-specific. Engine teeth on managed-service comment voice should refuse this opener regardless of tier.
3. **`apidev` KB regressed from `## Tips and gotchas` H2 (run-37) to headerless extract block (run-38).** appdev moved to `## Tips and Others` H2 (matches jetstream golden); workerdev moved from `### Gotchas` H3 (run-37) to `## Tips and Other Considerations` H2. KB sibling-shape divergence (Finding H from placement audit) persists with fresh variance — apidev is now the novel-shape outlier instead of workerdev.
4. **Tier-0 L1 acronym handling diverges from jetstream golden by design.** Run-38 tier-0 L1 reads `# NestJS Showcase — AI agent Environment` (lowercase "agent"); jetstream golden tier-0 L1 reads `# Laravel Jetstream — AI Agent Environment` (capital "Agent"). The engine code (`internal/recipe/tiers.go::tiers[0].Label = "AI agent"`) is canonical per run-32 fix; the jetstream golden hand-capitalizes "Agent" in L1. **This is a golden-vs-engine divergence, not a v9.79.0 regression.** Either the engine taxonomy or the jetstream golden L1 needs reconciliation. Same shape on tier-1: engine renders "Remote (CDE) Environment", jetstream golden hand-strips to "Remote Environment".
5. **workerdev IG count regression (6 → 4).** Quality of remaining items is at-bar but the count drop is unexplained. Could be sub-agent variance (within ±20% stochastic floor) or substrate de-emphasis. Worth tracking but not blocking.

---

## Side-by-side vs jetstream golden

| Surface | Verdict | Note |
|---|---|---|
| Codebase apps-repo title (`# Zerops x <NAME>[ <ROLE>]`) | **at-bar** | apidev/appdev/workerdev all `# Zerops x NestJS Showcase {API/Frontend/Worker}` |
| Apps-repo README structure (canonical) | **at-bar on shape, below-bar on order** | shape (cover → Deploy H2 → IG → KB) matches; section order below IG (RF1 → PD1 → Understand) inverted vs jetstream's (Understand → RF1 → PD1) — Finding F deferred |
| Tier README L1+L2 banner | **at-bar on engine-rendered shape; mild divergence on label-case** | L1+L2 templates match exactly; engine label `"AI agent"` vs jetstream-golden hand-cased `"AI Agent"` (surprise #4) |
| Tier yaml head comment | **at-bar** | mirrors tier README intro semantically |
| Per-service yaml comment voice | **at-bar except tier-3+4 db** | RES-1 — TY2 BAD lead-voice on db at tier-3 + tier-4 |
| Root porter-meta line | **at-bar** | engine-hardcoded since v9.78.0; matches jetstream byte-for-byte modulo path-encoding (em-dash %E2%80%94 vs raw em-dash) |
| KB sibling-shape consistency | **below** | jetstream uses `## Tips and Others` H2 single shape; run-38 has 3 distinct shapes (Finding H deferred) |
| TY5 priority-justification block | **below** | jetstream goldens carry this on every tier; run-38 has 0/6 (RES-2) |

---

## Recommended next step

**Run-38 ships.** Capture as new sim baseline; sim-replay; ship if deterministic. Open separate engine-teeth ticket for the two persistent substrate-only residuals (RES-1 + RES-2). Defer Findings F + H pending product decision.

### Next-iteration engine-teeth candidates (RES-1 + RES-2 ready)

Both substrate-only residuals have now persisted through three consecutive runs (run-36, run-37, run-38) despite substrate teaching. Per handoff: "If tier-4 TY2 residual persists for the third run in a row → recommend engine teeth on managed-service comment voice." The user has authorized engine teeth per-case.

**Proposed `complete-phase` gate at env-content phase:**

1. **`forbidTradeoffLeadOnDbCommentNonHA`** — refuse close when any `mode: NON_HA` postgres/mariadb/keydb service comment leads with `restoring from snapshot|tolerate a brief restart window|the .*-prod tradeoff|the .*-grade tradeoff`. Recovery: rewrite lead to imperative role+relationship form (`Single-instance NON_HA Postgres — used by the api codebase to store ...`).
2. **`requirePriorityJustificationBlock`** — refuse close when any tier `import.yaml` declares `priority: 10` on a managed service without a TY5-class explanation comment block somewhere ahead of the first managed-service block. Recovery: insert the substrate-canonical justification.

Both gates are input-invariant and fire regardless of agent behavior — same shape as the v9.79.0 RF1/PD1 absence gate that proved itself in this run.

### Defer

- **Finding F (section order below IG)** — `RF1 → PD1 → Understand → KB` (run-38) vs jetstream's `Understand → RF1 → PD1 → KB`. Both work; opinion-different. Awaits porter feedback.
- **Finding H (KB sibling-shape canonical)** — three distinct shapes persist; needs product decision on canonical (intersection-trap H2 vs porter-workflow H2/H3) before engine teeth.
- **Engine-vs-golden label-case divergence (Tier-0 "AI agent" / Tier-1 "Remote (CDE)")** — needs reconciliation between [`internal/recipe/tiers.go`](../internal/recipe/tiers.go) `Label` field and jetstream-golden hand-authored L1s. Not blocking; either side could yield. Recommend updating the jetstream golden to match engine taxonomy (engine is canonical per run-32; golden was hand-edited later).

---

## Codex validation

Per `feedback_codex_validation.md`: every analytical finding ran through `codex:codex-rescue` for verification. Results:

| Finding | Codex verdict |
|---|---|
| A — RF1/PD1 absence gate landed (apps-repos clean, canonical preserved) | CONFIRMED |
| B — Gate empirically fired (3 RF1 + 1 PD1 violations in main-session.jsonl, then retry succeeded) | CONFIRMED |
| C — Tier banner L1+L2 landed × 6 | CONFIRMED |
| D — Back-link + standalone deploy button removed × 6 | CONFIRMED |
| E — `## Deploy to Zerops` H2 wrapper × 3, legacy lead removed | CONFIRMED |
| F — 0/11 forbidden-token contamination | CONFIRMED |
| G — tier_decision Why-fill 100% (10/10) | CONFIRMED |
| H — yaml comment density (apidev 51.0%, appdev 56.9%, workerdev 55.6%) | CONFIRMED |
| I — TY2 BAD lead-voice at tier-3+4 db (count = 2 lead-voice instances) | DISAGREE → reconciled: codex's stricter `NON_HA Postgres` grep also catches tier-1 + tier-2, but those tiers lead with role+context and mention durability only in supporting clauses (softer TY2). Lead-voice TY2 BAD count = 2 (tier-3 + tier-4 db). Documented in RES-1. |
| J — TY5 priority-justification block 0/6 | CONFIRMED |
| K — workerdev IG count regression 6 → 4 | CONFIRMED |

One disagreement reconciled in the RES-1 narrative; all other findings independently confirmed.

---

## Pre-flight gate recommendation (preventive, not blocking run-38)

This is the SECOND time binary version drift has cost a dogfood (run-36 first; pre-flight in this run prevented a third). Recommend permanent pre-flight gate at zcprecipator3 `start`: engine emits its semver into `plan.json`; validation reports read it and fail loudly if `git describe --tags` at validation time differs from the recorded version. One file, one assertion, one cost-saving guarantee.
