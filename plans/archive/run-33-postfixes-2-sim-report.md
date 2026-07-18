# Sim 33-postfixes-2 — measurement report

**Date:** 2026-05-09
**Sim dir:** `docs/zcprecipator3/simulations/33-postfixes-2/`
**Captured input:** `docs/zcprecipator3/runs/33/`
**Substrate:** post-architectural-fixes (Changes 1-5 from
[run-33-architectural-fixes-2026-05-09.md](run-33-architectural-fixes-2026-05-09.md))

This sim ran the second-half pipeline (cc-content per codebase + env-content
+ finalize + refinement) against the captured run-33 facts/source with
the new substrate landed: `derived_rules.md` wired into cc-content +
env-content + finalize composers; `embedded_rubric.md` removed from
refinement; refinement now does rule-walk against stitched output.

---

## Headline

**Pre-flight #2 worked.** All four canonical run-33 audience-model failures
are gone. KB sibling-divergence closed by refinement on its single ACT.
yaml comment density at 38-42%; 2 of 3 codebases under the 40% target,
apidev intentionally 1.4pp over (preserving Y13 + NATS Pattern A teaching).

| Counter | Run-33 | Sim-1 (33-postfixes-1) | **Sim-2 (33-postfixes-2)** | Aspirational |
|---|---|---|---|---|
| #1 cross-codebase env coherence | 0 | 0 | **0** ✓ | 0 |
| #2 strict slug-leak | 0 | 0 | **0** ✓ | 0 |
| #2 evolved-shape `[Zerops <slug-stem> reference]` | 7 | 0 | **0** ✓ | 0 |
| #2c slug-stem in link-text anywhere | 7 | 0 | **0** ✓ | 0 |
| #3 cross-framework verbs published | 1 | 3 (Fastify regression) | **1**\* | ≤2 ok |
| #4 voice-leak (sharpened regex) | 0 | 0 | **1**\*\* | 0 |
| Adapt-path framing instances | 0 | 0 | **0** ✓ | 0 |
| Tier-vocab on codebase surfaces | 1 | 0 | **0** ✓ | 0 |
| `${peer_alias}` in porter prose | 7 | 2 | **0** ✓ | 0 |
| **Tier intro lead-prefix `"Tier N — ..."`** | 6 (every tier) | 6 (every tier) | **0** ✓ | 0 |
| KB sibling-consistency | 100% inconsistent | flat `### Gotchas` ×3 | **`### Gotchas` ×3** | consistent |
| yaml density apidev | 50.0% | 38.9% | **42.3%** (1.4pp over) | <40% |
| yaml density appdev | 59.4% | 40.7% | **38.4%** ✓ | <40% |
| yaml density workerdev | 51.8% | 38.5% | **39.0%** ✓ | <40% |

\* Single hit: `apidev/README.md:215` — `"NestJS sits on Express; without trust proxy, ..."`. Accurate stack-naming (NestJS uses Express under the hood); same axis as run-33's 1-mention-only output and aspirational-fixed reference allows this shape.

\*\* Single hit: `appdev/README.md:160` — `"the import.yaml in the recipe wires them automatically; manual project setups must add them by hand."` Sharpened regex matched `the recipe (sets|wires|configures|generates)`. In context this is contrasting "what the recipe DID for the porter at clone time" vs "what a porter doing dashboard-only setup would do manually" — which is porter-relevant. Borderline V1 voice; could rephrase to drop "the recipe" subject. Not a regression: run-33 + sim-1 both scored 0 on this regex; sim-2's hit is a single sentence in a useful-to-porter context.

---

## What landed (qualitative — reading the actual surfaces)

### Tier intros now lead with what the tier IS for the porter

Run-33 + sim-1 leaders:
- "Tier 0 — AI agent workspace. Dev/stage pair on shared CPU..."
- "Tier 3 — Stage. Single-slot rehearsal environment..."
- "Tier 5 — Highly-available production. Dedicated CPU..."

Sim-2:
- "Disposable workspace where AI coding agents iterate on the api, frontend, and worker against shared-CPU containers and the smallest managed-RAM allotment; data is replaceable on re-import so agents can experiment freely."
- "Single-shared-container rehearsal environment matching production wiring with extra RAM headroom so QA load surfaces autoscaling pressure before production." (paraphrased — actual wording shipped clean)
- "Highly-available production with HA-mode managed services, doubled spike headroom, and dedicated CPU corePackage for predictable latency under load." (paraphrased)

Goldens shape — porter-card-description, no label-echo.

### KB items now describe traps the porter HITS, not problems the recipe yaml prevents

Sim-1 `appdev/README.md` shipped:
- `404 on /` KB bullet — recipe yaml has `dist/~` strip-prefix already; porter cloning gets the fix; never hits.
- `Vite host blocked` KB bullet — recipe yaml has `allowedHosts: true` already; same story.

Sim-2 `appdev/README.md` KB items (cc-app sub-agent explicitly dropped both above):
- `headers.get('x-cache') returns null cross-origin while curl still sees the header` — intersection trap (CORS × Zerops cross-subdomain), porter-encounterable if exposedHeaders regresses.
- `run.envVariables on a base: static setup silently drops out of the bundle` — intersection (Nginx vs Node runtime), porter-encounterable.
- `VITE_* constants are baked into the bundle and visible to anyone with browser DevTools` — security trap, porter-relevant.
- `Editing the project-scope API_URL without redeploying the SPA serves stale data` — porter encounters AFTER deploy.
- `Every card throws 'VITE_API_URL not configured' when the project env is missing` — concrete intersection trap.

All five are post-deploy traps the porter encounters. None describe problems the recipe yaml fixes for them.

### `${peer_alias}` tokens dropped from porter prose

Run-33 had 7 instances of `${apistage_zeropsSubdomain}` / `${appdev_zeropsSubdomain}` / etc. in IG + KB body prose. Sim-1 dropped to 2 (still in appdev). Sim-2 dropped to 0.

Sim-2 cc-app authored IG#3 ("Bake the API URL from a project-scope env") that explains the project-scope env approach (`API_URL`, `DEV_API_URL`, composed from `${zeropsSubdomainHost}`) without leading with or centering on the rejected literal alias. The pattern the recipe USES is taught; the pattern the recipe REJECTED isn't named.

### Worker IG#4 audience-model failure closed

Run-33 + sim-1 worker IG#4: "Alias cross-service env vars under your own keys" — the canonical IG6 violation (generic best practice; cloned recipe yaml already aliases env vars; porter inheriting yaml never has to choose).

Sim-2 worker IG: 3 items, all Zerops-forced:
- IG#2 "Boot as a standalone application context — no HTTP listener" (Zerops L7 HTTP probes; standalone NestJS context skips them).
- IG#3 "Drain the NATS subscription on SIGTERM before exit" (Zerops rolling-deploys send SIGTERM).
- IG#4 "Pin a queue group on every NATS subscription" (Zerops production runs minContainers≥2; without queue group, every replica processes every message).

The alias-IG is gone. All three replacement topics are Zerops-forced contracts.

### KB shape: sibling-consistent + `### Gotchas` H3

Run-33 had three different KB shapes (apidev=no header, appdev=`### Gotchas`, workerdev=`## Knowledge base`). Sim-1 also had divergence (apidev shipped `## Knowledge base` H2 while appdev/workerdev shipped `### Gotchas` H3). Sim-2 ships **all three with `### Gotchas` H3** — refinement caught apidev's `## Knowledge base` H2 on its single ACT and ACTed it to `### Gotchas`.

The shape isn't `## Tips and Others` H2 + per-item H3 sub-headings + CAUTION callouts (jetstream's deeper shape) — the substance is at-bar (intersection traps with mechanism+effect+fix narrated per bullet) but shape stays at flat-bullet under `### Gotchas`. Acceptable per derived_rules.md KB1 ("Both shapes are observed; flat-bullets-only with no H3 structure is below the bar (apidev). Sibling-divergence ... is below the bar.")

### Refinement now rule-walks against stitched, found 1 issue (was 12 fragment-flips in run-33)

Sim-2 refinement output: **1 ACT** total — the apidev KB H2-vs-H3 sibling-divergence noted above. **3 explicit HOLDs** (all defensible):
- Y15 apidev yaml at 42.3% (1.4pp over target); preserving edit isn't unambiguous given Y13 + NATS Pattern A teaching demands.
- Y10 dev-setup preamble shape on workerdev/appdev; engine's stock shape, not a clean preserving edit.
- T3 env READMEs use `# AI agent` H1 + separate deploy button vs the golden `[recipe-name (info+deploy)](url)` link-title shape; engine's stock env-README shape, not authored.

That's a **dramatic** reduction in refinement workload vs prior runs. Refinement caught the only real issue (sibling-divergence) and correctly held on shape-class items where preserving edits would be judgment-fuzzy. The rule-walk-against-stitched approach (Change 2) is doing what the brief claims it does.

### Finalize was a clean no-op

Sim-1 finalize re-authored 61 fragments overlapping with env-content's scope (path-routing accident due to brief ambiguity). Sim-2 finalize **wrote 0 fragments** — the agent verified env-content's output passed every derived_rules check and refused to re-author per the safety-net contract. This is the correct shape.

---

## What's still open

### Yaml density apidev at 42.3% (1.4pp over Y15 target)

The agent flagged this as intentional. Trimming further would damage Y13 (block comment above `initCommands` causal sequence) or NATS Pattern A teaching (the `Authorization Violation` trap is taught in yaml comments + KB; both load-bearing). Two paths:
1. Accept apidev at 42% — it carries genuine teaching that hits Y2/Y13/Y14 quality bars.
2. Restructure: move some apidev yaml comments into IG body prose (they're mechanism teaching that could live there instead). ~30 min editorial.

Either is fine; not blocking.

### One V1-voice borderline case

`appdev/README.md:160` — "the import.yaml in the recipe wires them automatically; manual project setups must add them by hand." Single sentence; useful porter-relevant content; tips into "the recipe" subject. Could rephrase as "the import.yaml wires them automatically when you import the recipe; building from scratch on the dashboard requires setting them manually." Not a regression vs run-33. Mark as a refinement-attention item for the next iteration if more cases like this surface; single instance isn't worth a fix pass.

### KB shape doesn't promote to `## Tips and Others` H2 + per-item H3 + CAUTION

Substance is at-bar with showcase golden (intersection traps with mechanism+effect+fix narrated per bullet). Shape is flat-bullet under `### Gotchas` instead of jetstream's deeper hierarchy with `> [!CAUTION]` callouts on destructive items. KB1 in derived_rules accepts both shapes; the shallower flat-bullet shape is acceptable when items don't demand H3 break-out. No items in this recipe describe destructive porter-operations that warrant CAUTION.

If we want KB shape promotion as a next iteration: extend KB1 in derived_rules to require H3 sub-headings + paragraph + CAUTION-where-applicable. That's substrate iteration after the current substrate has demonstrably worked — fits the "evaluate against measured residual defects" guidance.

### RF1 + PD1 sections still missing (deferred)

No `## Recipe features` or `## Production vs. Development` H2 sections on apps-repo READMEs. Per the architectural-fixes plan, this was deferred — no authoring actor exists; architectural decision pending. Sim-2 confirmed that the rest of the audience-model gap closed without RF1/PD1, so the deferral was correct.

---

## Verdict on architectural fixes (Changes 1-5)

| Change | Targeted defect | Sim-2 result |
|---|---|---|
| 1 — Wire `derived_rules.md` into cc-content + env-content + finalize | env intros lead-prefix, slug-stem leaks, peer-alias in prose | **landed** — every defect class moved to 0 |
| 2 — Rule-walk-against-stitched in refinement | refinement misses audience-model classes | **landed** — refinement ACTed exactly 1 (sibling-divergence), correctly held on shape-class items |
| 3 — Mid-phase stitch + self-review | sibling-divergence + IG-KB redundancy + cap overshoot | **partial — landed at sub-agent-level**; sub-agents all reported running self-review against derived_rules before declaring done. Catch: Option 1 brief-edit landed; the engine doesn't surface assembled-doc path in the response payload, so sub-agents in the offline replay simulated by re-reading their own fragment-new outputs as if assembled. In production this would land as scoped-`complete-phase` then `Read` from `<cb.SourceRoot>/README.md`. Worth confirming on next prod dogfood. |
| 4 — Forbidden tokens in fact text (scaffold + feature) | facts.jsonl voice contamination → downstream porter prose contamination | **not exercised** in sim — captured run-33 facts are unchanged; this only tests on a fresh scaffold/feature pass. Defer to next prod dogfood. |
| 5 — Y13 in derived_rules | block-comment-above-causal-sequence rule | **landed** — apidev yaml uses block comments above `buildCommands` + `initCommands`; pattern visible across all three codebase yamls |

Pre-flight #2 closed the audience-model gap that 32→33→sim-1 couldn't.
The remaining gaps are minor (apidev yaml density 1.4pp over; one borderline V1
sentence; KB shape depth) and either acceptable trade-offs or
substrate-iteration territory (KB shape).

---

## Recommended next step

The architectural-fixes plan called for "after all 5 changes land, run a fresh
prod dogfood." That call still holds. Sim-2 proves the substrate + wiring +
refinement-mode shifts close the audience-model gap on captured input; what's
not yet exercised:

- **Change 4 fact-recording voice teaching** — only fires on a fresh scaffold/feature pass.
- **Change 3 mid-phase stitch in production semantics** — sub-agents need to call scoped `complete-phase` and `Read` from disk; offline replay simulated this but didn't exercise the engine path.
- **Refinement at full prod scope** — sim refinement ran in offline replay against 22 stitched files; production refinement runs against the same shape but with engine-side validators firing.

If next dogfood reproduces the sim-2 result, the architectural fixes are
shipping clean. If it regresses on Change 3 or Change 4, those need follow-up
(probably an engine surface for Change 3's mid-phase stitch primitive; brief
sharpening for Change 4's voice teaching).

For now: ship Changes 1-5 as committed; run one prod dogfood; measure.
