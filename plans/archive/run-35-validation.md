# Run-35 validation — first prod dogfood after v9.77.0 fix-pack

**Date:** 2026-05-09
**Run dir:** [`docs/zcprecipator3/runs/35/`](../docs/zcprecipator3/runs/35/)
**Substrate:** v9.77.0 — 5 engine/brief fixes + 2 cleanups closing
[run-34 regressions](run-34-validation.md). Commits `09a81b3f`,
`8355b076`, `9a40318c`, `2f10244c`, `0dc7010d`, `142bd2a8`, `1e8d18b8`,
`e9d762a9`, `b7f34555`, `1cb10e1c`.
**Sim baseline:** [`docs/zcprecipator3/simulations/33-postfixes-3/`](../docs/zcprecipator3/simulations/33-postfixes-3/)
**Codex-verified:** every claim below confirmed via `codex:codex-rescue`
(yaml-density numbers and IG6 judgment call double-checked after a
first-pass measurement disagreement; codex retracted on re-run).

---

## Headline

**Production-ready candidate. All five v9.77.0 fixes landed in
production semantics; audience-model substantially at-bar with sim-3;
the three sim-3-win regressions from run-34 are closed.** One residual
sim-3-win lost (KB sibling header-shape consistency), one partial
(IG#4 title is named-trap framing, body still teaches the IG6
alias-under-own-keys mechanic), and three deferred items measured
without iteration. **Recommend marking run-35 as the new sim baseline;
sim-replay against captured facts.jsonl + plan.json + scaffold output
to verify reproducibility; ship if clean.**

The engine teeth shipped in v9.77.0 (preStitchEnv + RF1/PD1 gate +
record-fact forbidden-token gate) all fired correctly on run-35 inputs.
The two substrate-only fixes (refinement rule-walk teaching +
cc-content disk-Read MANDATORY framing) also landed at runtime —
substrate compliance held under fresh inputs, partly because the
agent had also already authored the required content.

---

## Counter table

| Counter | Run-34 floor | Sim-3 success-floor | **Run-35 actual** | Verdict |
|---|---|---|---|---|
| #1 cross-codebase env coherence | 2 mismatches | 0 | **0** | sim-3 floor — REGRESSION CLOSED |
| #2 strict slug-leak | 0 | 0 | **0** | at-bar |
| #2c slug-stem in link-text | 0 | 0 | **0** | at-bar |
| #3 cross-framework verb count | 1 (legit) | 1 (legit) | **3** (NestExpress / Express adapter — legit per codex) | at-bar (NestJS uses Express) |
| #4 sharpened voice-leak | 0 | 0 | **0** | at-bar |
| Adapt-path framing | 0 | 0 | **0** | at-bar |
| `${peer_alias}` in porter prose | 3 (trap-explanation) | 0 | **7** (all in trap-explanation context: 4 apidev IG/KB, 3 appdev IG/yaml) | trap-teaching shape, not porter-prose leakage |
| Tier intro `Tier N — ` prefix | 6 | 0 | **0** | **REGRESSION CLOSED** (sim-3 floor) |
| KB sibling-consistency | 3 distinct H2 | `### Gotchas` H3 ×3 | **3 distinct shapes** (apidev `### Gotchas` H3; appdev no H2 wrapper, bare `### <topic>` H3 entries; workerdev `## Knowledge Base` H2) | sim-3 win **LOST** |
| yaml density apidev | 45.5% | 38.9% | **46.6%** | marginal regression vs run-34, deferred |
| yaml density appdev | 61.7% | 38.4% | **63.5%** | marginal regression vs run-34, deferred |
| yaml density workerdev | 48.3% | 36.1% | **53.8%** | regression vs run-34 (+5.5pp), deferred |
| RF1 (`## Recipe features`) on apidev | absent | present | **present** (line 248) | **REGRESSION CLOSED** |
| PD1 (`## Production vs. Development`) on apidev | absent | present | **present** (line 259) | **REGRESSION CLOSED** |
| `## Understand Zerops Core Concepts` on apidev | absent | present | **present** (line 265) | **REGRESSION CLOSED** |
| facts.jsonl strict-token contamination | 15/117 (12.8%) | not exercised | **0/112 (0%)** | **GATE FIRED — full closure** |
| tier_decision Why-fill | not measured | n/a | **10/10 (100%)** (run-32 baseline = 0/10) | full closure of run-32 baseline gap |

**Six regression rows from run-34 closed cleanly.** Three deferred
yaml-density rows show marginal-to-mild regression but stay in the
"measured, not iterated" bucket per user direction. KB sibling
consistency is the one sim-3 win that didn't reach run-35.

---

## Per-surface porter-empathy read

### apidev/README.md — at-bar with sim-3, structurally complete

What a porter sees:
- Intro: clean. Names framework + capabilities (NestJS REST API + items CRUD + 5 managed services).
- IG #1 (Adding `zerops.yaml`): 182-line annotated yaml block. Comment density 46.6% — above target, similar to run-34, deferred per user.
- IG #2-#5: bind 0.0.0.0 / trust proxy / **avoid self-shadow on auto-injected env vars** / decompose execOnce keys. IG #2-#3 + IG #5 are clean named-trap teaching. IG #4 is reframed self-shadow trap (run-34 was a generic alias-IG6 violation); body still teaches the alias-under-own-keys mechanic at line 230 ("The cloned `zerops.yaml` already wires every cross-service reference under a fresh own-key…"). **Codex verdict: not a clean IG6 escape — title is named-trap framing, body retains the IG6 mechanic.**
- `## Recipe features` H2 present (RF1).
- `## Production vs. Development` H2 present (PD1).
- `## Understand Zerops Core Concepts` H2 present.
- KB at `### Gotchas` H3: 8 bullets, intersection-trap shape, all post-deploy traps a porter would actually hit (TypeORM synchronize, APP_SECRET self-shadow, CORS_ORIGINS bake-time literal, X-Cache cross-origin invisibility, NoSuchBucket virtual-host trap, UnknownError on storage endpoint scheme, empty Meilisearch on redeploy, 502 on loopback bind). Substance at-or-above sim-3.

The published apidev README is **structurally complete vs sim-3** —
the three H2 sections that vanished in run-34 are back.

### appdev/README.md — at-bar substance, KB shape divergent

What a porter sees:
- Intro: clean.
- IG #1-#4: zerops.yaml / bake VITE_API_URL at build / strip dist/~ deploy prefix / bind Vite dev server. All Zerops-forced or recipe-feature topics. **No alias-IG.** Topical at-bar with sim-3.
- `## Understand Zerops Core Concepts` H2 present.
- KB: bare `### <Specific Topic>` H3 entries with no H2 wrapper. Three entries (Vite host allowlist, Mixed-Content HMR, X-Cache cross-origin invisibility). Substance at-bar.
- yaml at 63.5% comment density — way over target (jetstream golden 36%). **Worst of the three codebases.** Still-loose item, deferred.

### workerdev/README.md — at-bar substance, KB shape divergent

What a porter sees:
- Intro: clean.
- IG #1-#5: zerops.yaml / NATS Pattern A connect / standalone application context / **avoid self-shadow** / drain-on-SIGTERM. IG#4 same self-shadow framing as apidev (named-trap title, IG6-mechanic body). Topical at-bar.
- `## Understand Zerops Core Concepts` H2 present.
- KB at `## Knowledge Base` H2 — 5 bullets covering queue-group fan-out, drain-vs-unsubscribe, NATS double-auth, missing env wire on new services, APP_SECRET self-shadow. Substance at-bar.

### Tier READMEs — every tier clean

All 6 tier intros lead with porter-card descriptions, no `Tier N — ` prefix:
- Tier 0: "Agent workspace shape — single-instance managed services on shared CPU…"
- Tier 1: "Remote workspace for a human developer (CDE) — same single-instance shape…"
- Tier 2: "Runs the production setup of every codebase against single-instance managed services on shared CPU…"
- Tier 3: "Production setup on shared CPU with single-instance managed services…"
- Tier 4: "Two runtime containers per codebase on shared CPU enable rolling deploys with zero downtime…"
- Tier 5: "Dedicated CPU, SERIOUS corePackage on the project, HA mode on db/cache/broker…"

The full **REGRESSION CLOSED** vs run-34 — preStitchEnv fix landed
end-to-end, refinement's 6 env/N/intro replaces persisted to disk.
Voice still tilts toward operational-engineer language ("dev-runtime
containers", "0.25 GB free-RAM headroom buffer") rather than
porter-card framing of intent-of-use, but the run-34 BAD prefix shape
is gone.

### Tier import.yamls — managed-service comments still TY2-shape

Stage tier db comment (run-35):
> "Single-instance Postgres — restoring from snapshot still means downtime, the rehearsal-grade tradeoff at stage. The 0.25 GB minFreeRamGB headroom keeps query bursts off the swap path; bump verticalAutoscaling.minRam when stage workload pushes query latency past your SLO."

Sim-3:
> "Single-node PostgreSQL — used by the api codebase to store items + job logs. Snapshots-only durability at this rehearsal tier; bump `verticalAutoscaling.minRam` if your stage workload pushes query latency."

Jetstream golden:
> "Deploy single node PostgreSQL database, used by the Laravel app to store data. Automatic, encrypted backups are enabled by default."

Run-35 leads with durability/HA tradeoff (codex sharpening: not
strictly "scaling math" — that's the second sentence) but still NOT
the plain-English service intro + framework-bridge shape jetstream
uses. Same TY2 BAD pattern persists. Deferred per user.

### Root README — at-bar with sim-3

R1-R6 shape clean: intro extract → deploy button → cover → 6-tier list
→ catalog punt → Discord. No `## H2` content sections. 25 lines.
**Correctly does NOT contain RF1 + PD1** (per the gate scope — those
belong on apps-repo/apidev only).

---

## Fix verification matrix

| Fix shipped (v9.77.0) | Brief edit landed | Production semantics landed | Verdict |
|---|---|---|---|
| 1 — preStitchEnv parity (`09a81b3f`, `b7f34555`) | yes (engine) | **yes** — 6 refinement env/N/intro replaces persisted to disk; all 6 published tier intros are the refined bodies, not the priorBody | **LANDED** |
| 2 — Un-slotted IG always stitches + RF1/PD1 engine gate (`8355b076`, `9a40318c`) | yes (engine) | **yes** — RF1+PD1 present in apidev, absent in appdev/workerdev (correct gate scope). Gate exists at `internal/recipe/gate_canonical_apps_repo.go:82-103` (codex-verified). Gate didn't refuse this run because cc-api authored both per the brief teaching; teaching + gate are belt-and-suspenders | **LANDED** (gate primed, agent complied) |
| 3 — record-fact forbidden-token gate (`0dc7010d`) | yes (engine) | **yes** — 0/112 contamination across all 11 forbidden tokens. Run-34 was 15/117 = 12.8%, run-32 baseline 12% | **LANDED — full closure** |
| 4 — Refinement rule-walk teaching (`142bd2a8`, `1e8d18b8`) | yes (substrate) | **yes** — refinement subagent log carries 8 rule-walk + 13 derived_rules hits; rubric/criterion mentions (5/1/2) all in retirement-explanation context only; embedded_rubric (1 hit) likewise meta-narrative | **LANDED** |
| 5 — cc-content disk-Read MANDATORY (`2f10244c`) | yes (substrate) | **yes** — all three cc-content sub-agents Read `<cb.SourceRoot>/README.md` ≥1 time after scoped complete-phase. cc-api 2 reads (run-34 was 0); cc-app 2; cc-worker 1. Run-34's 0-read asymmetry on apidev is closed; absolute reads dropped (run-34 had 12 total; run-35 has 5) but the floor moved from 0 to 1 | **LANDED — minimum threshold met** |

**Five for five.** Three engine fixes landed at the boundary regardless
of agent behavior; two substrate-only fixes landed because the agents
complied at runtime. Substrate compliance held under fresh inputs in
run-35 — but is structurally less guaranteed than engine teeth, so a
future regression on either substrate fix would warrant moving the
enforcement to engine.

---

## Three still-loose items measured

User explicitly deferred these in v9.77.0 — verdict is at-or-similar to
run-34 unless catastrophic regression.

| Item | Run-34 | Run-35 | Verdict |
|---|---|---|---|
| Yaml comment density (codebase zerops.yaml) | apidev 45.5% / appdev 61.7% / workerdev 48.3% | apidev 46.6% / appdev 63.5% / workerdev 53.8% | **marginal regression** (+1.1 / +1.8 / +5.5pp); workerdev is the worst delta. Stays deferred. |
| Tier yaml managed-service comment voice (TY2) | leads with operational tradeoff + scaling math | same shape — leads with durability/HA tradeoff, scaling/headroom in second sentence | **at-bar** — TY2 BAD pattern persists. Stays deferred. |
| Cross-codebase env coherence (S3_*/STORAGE_*, MEILI_*/SEARCH_*) — `gate_cross_codebase_env_coherence.go` at `SeverityNotice` only | 2 mismatches | **0 mismatches** — apidev + workerdev both use identical aliases (`CACHE_*`, `NATS_*`, `MEILI_*`) for the shared services | **CLOSED** (incidentally; gate severity unchanged) |

Net: one item closed (env coherence), two stable (TY2 voice, yaml
density — the latter mildly worsened on workerdev). None catastrophic.

---

## Specific defect classes — verify-absent results

| Class | Run-33 | Sim-3 | Run-34 | **Run-35** |
|---|---|---|---|---|
| Tier intros leading with `Tier N — ` | 6 | 0 | 6 | **0** |
| `${<peer>_zeropsSubdomain}` in porter prose | 7 | 0 | 3 (trap-explanation) | **7** (all trap-explanation context) |
| KB describing already-fixed problems | 3 cases | 0 | 0 | **0** |
| `[Zerops <slug-stem> reference/guide/service]` link-text | 7 | 0 | 0 | **0** |
| Cross-recipe references | 1 | 0 | 0 | **0** |
| IG6 violations (alias-IG generic best practice) | 1 (worker) | 0 | 2 (apidev IG#4 + worker IG#4 — generic alias-IG titles + bodies) | **partial: 2 IG#4s reframed as named self-shadow trap (titles clean), bodies still teach alias-under-own-keys mechanic** |
| RF1 / PD1 / Understand on apidev | absent | present | absent | **present** |

The IG6 partial framing matches codex's verdict: title is "Avoid the
self-shadow when re-declaring auto-injected env vars" (a real platform
trap, distinct from generic alias-IG); body at line 230 still says
"The cloned zerops.yaml already wires every cross-service reference
under a fresh own-key… so application code reads stable names." The
body teaches the IG6 mechanic; the title teaches the named trap. Two
audiences, same body. Whether to count this as IG6 closed or IG6
partial depends on framing — best-honest framing is **partial closure
at the title axis, residual at the body axis**.

The 7 `${peer_alias}` mentions in run-35 are all in trap-explanation
context (apidev L126 yaml-comment trap warning, L277 KB CORS_ORIGINS
trap; appdev L56 yaml-comment trap warning, L112 IG#2 paragraph
explaining the bake-time trap). More mentions than run-34 — but
pedagogically *more* correct (the substrate teaches the trap explicitly
rather than silently using the safe shape). Counter score-only would
flag this as regression; semantic read says it's improved teaching.

---

## Side-by-side voice comparison vs jetstream

| Surface | Jetstream golden | **Run-35** | Verdict |
|---|---|---|---|
| Tier-3 README intro | "**Stage** environment uses the same configuration as production, but runs on the lowest scaling settings." (delta-shape, plain) | "Production setup on shared CPU with single-instance managed services and a 0.25 GB free-RAM headroom buffer on every container, sized for rehearsal traffic and QA runs against snapshots-only durability." | **below** — content at-bar, voice still tilts operational-engineer rather than porter-card delta |
| Tier-3 db comment | "Deploy single node PostgreSQL database, used by the Laravel app to store data. Automatic, encrypted backups are enabled by default." (plain-English service intro + framework bridge) | "Single-instance Postgres — restoring from snapshot still means downtime, the rehearsal-grade tradeoff at stage. The 0.25 GB minFreeRamGB headroom keeps query bursts off the swap path…" | **below** — TY2 BAD pattern (durability tradeoff lead, scaling math second). Deferred. |
| apps-repo H2 structure | `## Integration Guide` / `## Understand Zerops Core Concepts` / `## Recipe features` / `## Production vs. Development` / `## Tips and Others` | apidev: `## Integration Guide` / `## Recipe features` / `## Production vs. Development` / `## Understand Zerops Core Concepts` / `### Gotchas` | **at-bar with one shape divergence** — H2 set complete; KB uses `### Gotchas` H3 instead of `## Tips and Others` H2 wrapper |
| KB sibling consistency | n/a (single codebase) | 3 distinct shapes across siblings | sim-3 win lost |
| apidev/CLAUDE.md | framework-only, `claude /init`-shape | framework-only, `claude /init`-shape (verified line-by-line) | **at-bar** |

---

## Top 5 surprises

### 1. preStitchEnv fix landed end-to-end on first prod inputs

Refinement made 6 env/N/intro replaces in [`agent-ad77eb0d931fad605.jsonl`](../docs/zcprecipator3/runs/35/SESSION_LOGS/subagents/agent-ad77eb0d931fad605.jsonl);
all 6 published env READMEs at [`environments/*/README.md`](../docs/zcprecipator3/runs/35/environments/) carry
the new bodies, not the priorBody. Run-34's tier-prefix regression
(traced to the missing `preStitchCodebases` parity for env fragments)
is closed at the persistence boundary. Refinement also did 9 codebase
replaces (api/IG#4 ×2, api/KB ×2, app/IG full + app/IG#5, worker/IG#4,
worker/KB ×2) and those landed similarly — codebase preStitch already
worked, env preStitch now matches.

### 2. Forbidden-token gate caught everything; agents stopped trying

Run-34 had 15/117 contaminated facts (12.8%) — substrate teaching
landed in briefs but agents ignored at runtime. v9.77.0 promoted the
rule from substrate-teaches to engine-rejects. Run-35 facts.jsonl is
0/112 across all 11 patterns. **The agents didn't author any rejected
records this run** — the dispatch chain saw the new gate, the agent
adapted voice up-front. Engine-teeth-beats-substrate-teaching
hypothesis confirmed at first-prod scale.

### 3. apidev IG#4 reframing — title clean, body still IG6 mechanic

Run-34's IG#4 (apidev) was titled "alias managed-service env vars" —
a clean-textbook IG6 violation (generic best practice the cloned recipe
yaml already does). Run-35 IG#4 is "Avoid the self-shadow when
re-declaring auto-injected env vars" — a real named platform trap
(re-declare `db_hostname: ${db_hostname}` and the value becomes the
literal `${db_hostname}` string). The title is clean.

But the IG body retains the alias-under-own-keys teaching: "The cloned
`zerops.yaml` already wires every cross-service reference under a fresh
own-key (`DB_HOST: ${db_hostname}`, `API_SIGNING_KEY: ${APP_SECRET}`)
so application code reads stable names." The teaching outcome
(aliases-under-own-keys exists in cloned yaml) is the IG6 outcome; the
entry point (the shadow failure mode) is named-trap.

Codex called this "not a clean IG6 escape" — body still teaches the
mechanic. Two readings of the IG#4 are both defensible:
- **Pro-named-trap:** porter encounters the trap (`process.env.APP_SECRET`
  returns literal `${APP_SECRET}`), reaches the IG, learns the cause
  + the prevention. Pedagogically grounded.
- **IG6-residual:** body still says "the cloned yaml already does this",
  which is the IG6 smell — informing the porter about a fix the recipe
  has already applied for them.

For practical purposes: **partial closure**. Recommend not iterating
unless future runs regress to run-34's outright generic-alias title.

### 4. KB sibling consistency: 3 distinct shapes — sim-3 win lost

Sim-3 had `### Gotchas` H3 ×3 across all three codebases — clean shape
contract. Run-35 has:
- apidev: `### Gotchas` H3 (matches sim-3)
- appdev: bare `### <topic>` H3 entries with no H2 wrapper (no group label)
- workerdev: `## Knowledge Base` H2 (uses literal slot-vocab leak — internal engine-vocabulary)

This is the same class of failure that run-32 phase 1 baseline (Counter
#8) measured at 100% inconsistency — three parallel cc-content
sub-agents authoring KB sections without a shared shape contract.
v9.77.0 didn't address this; sim-3 closed it via runtime convergence
that didn't carry over into run-35.

The shape divergence has no engine teeth. cc-content briefs teach KB
content (citations, intersection-trap shape) but don't pin the wrapper-H2
title. **This is a candidate for a future engine gate** — define a KB
section title invariant (e.g. `### Gotchas` H3, no H2 wrapper, matching
jetstream's `## Tips and Others` ⇨ `### <topic>` H3 entries) at the
un-scoped cc complete-phase boundary, alongside RF1+PD1.

### 5. Workerdev yaml comment density worsened most (53.8%, +5.5pp vs run-34)

Run-34 was 48.3%; run-35 is 53.8%. Apidev moved +1.1pp, appdev +1.8pp,
workerdev +5.5pp. Sim-3 had workerdev at 36.1%. The worker yaml
introduces multiple paragraph-block comments (NATS Pattern A trap
explanation; queue-group fan-out semantics; SIGTERM drain rationale)
that are pedagogically valuable but push density past run-34. Deferred
per user.

---

## Diagnosis — fix verification

All five v9.77.0 fixes landed in production semantics on first prod
inputs. The substrate-only fixes (refinement rule-walk; cc-content
disk-Read MANDATORY) landed at runtime — both substrate-compliance
checks passed under fresh inputs. The three engine fixes (preStitchEnv,
RF1/PD1 gate, forbidden-token gate) fired correctly at the engine
boundary; one was actively triggered by agent compliance (forbidden
tokens — agents up-front avoided them), one was triggered by agent
non-compliance and recovery (cc-api complete-phase first call rejected
on 4 kb-citation-missing, second succeeded), and one was a persistence
fix that fired silently (preStitchEnv).

**No new defect classes from run-35 inputs.** The KB sibling-consistency
sim-3 win loss is a residual from run-32/run-33 era that v9.77.0 did
not target — not introduced by run-35 inputs.

---

## Recommended next step — mark run-35 as new sim baseline; sim-replay; ship if clean

The user's gate criteria from the handoff:
- "If at-or-above sim-3 across the audience-model axis AND all 5 fixes
  landed in production semantics → mark run-35 as the new sim baseline;
  capture facts.jsonl + plan.json + scaffold output as the new sim
  fixture; sim-replay against this fixture; if sim passes, ship and
  close iteration."

Both clauses are met:
1. **All 5 fixes landed in production semantics** — verified in fix
   matrix above.
2. **Audience-model substantially at-bar with sim-3:**
   - 3 sim-3 wins matched (RF1 + PD1 + Understand on apidev; clean
     tier intros; 0 voice-leak / 0 fact contamination — exceeded sim-3).
   - 1 sim-3 win lost (KB sibling consistency — three distinct shapes,
     not the consistent `### Gotchas` H3 ×3 sim-3 had).
   - 1 sim-3 partial (IG6 — title is named-trap, body still IG6
     mechanic; sim-3 had clean exit on both axes).

The **KB sibling-consistency loss is the only meaningful audience-model
regression vs sim-3**. Two paths:

**Path A — Ship now, iterate KB-shape later.** All run-34 regressions
closed; sim-3-win-loss on KB shape doesn't break porter usability (each
KB is internally coherent; the issue is cross-sibling drift). Mark
run-35 as new sim baseline; sim-replay against captured run-35 facts;
ship.

**Path B — Add KB sibling-shape engine gate before shipping.** The
shape contract is concrete (per-codebase `### Gotchas` H3 with no H2
wrapper, jetstream-style; OR `## Tips and Others` H2 with `###
<specific topic>` H3 entries; pick one and pin it). Engine gate at
un-scoped cc complete-phase fires alongside RF1/PD1. Cost: ~1-2 hours
engine + tests, sim-replay. Per user direction: "engine teeth beat
substrate teaching wherever both apply" + "user has authorized adding
engine teeth on a per-case basis".

**Recommended: Path A first** — capture run-35 as new sim baseline
NOW, sim-replay, ship. KB-shape engine gate is the next iteration's
first item if a follow-up dogfood regresses (or if the user wants to
push the audience-model bar higher than sim-3). Reasoning: the
five v9.77.0 fixes account for 6 closed regressions and 3 sim-3-win
recoveries; one residual from a class that pre-dates v9.77.0 is not
the bar to hold the iteration on.

If Path A: capture
[`environments/facts.jsonl`](../docs/zcprecipator3/runs/35/environments/facts.jsonl) (112 records, 0 contamination)
+ [`environments/plan.json`](../docs/zcprecipator3/runs/35/environments/plan.json)
+ scaffold output ([`apidev/`](../docs/zcprecipator3/runs/35/apidev/), [`appdev/`](../docs/zcprecipator3/runs/35/appdev/), [`workerdev/`](../docs/zcprecipator3/runs/35/workerdev/))
as `simulations/35-fixpack/`. Run sim-replay against captured fixture;
verify the same fix-matrix outcomes hold deterministically. If sim
passes, ship v9.77.0 + close iteration.

---

## What we're NOT doing

| Not doing | Why |
|---|---|
| KB sibling-shape engine gate this iteration | One sim-3 win lost; not a regression vs run-34. Shipping the closer-to-sim-3 v9.77.0 first; gate becomes next iteration's lead item if follow-up confirms persistence. |
| Substrate iteration on TY2 yaml-voice or yaml-density | Deferred per user. Persistent at-or-near run-34. |
| IG#4 body further refinement | Codex verdict is "partial IG6 smell"; title is meaningfully different from run-34's outright IG6 violation. Marginal-cost iteration. |
| Re-running another full prod dogfood before sim-replay | Captured run-35 fixture exercises every v9.77.0 path with verified outcomes. Sim-replay validates determinism faster + cheaper. |

---

## Verdict

**v9.77.0 is production-ready candidate.** All five engine/brief fixes
landed in production semantics. Six run-34 regressions closed. Three
sim-3 wins recovered. Two persistent gaps (KB sibling consistency,
TY2 yaml voice) are at-or-similar to prior runs and don't block ship.
One audience-model partial (IG6 title clean, body residual) is
acceptable.

Capture run-35 as new sim baseline, sim-replay against captured
fixture, ship + close iteration if sim deterministic.
