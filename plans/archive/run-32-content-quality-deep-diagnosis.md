# Run-32 deep diagnosis — recipe content-quality plateau (2026-05-08)

**Status:** Pre-flight execution complete. Diagnosis validated by Opus + Codex (independent passes), 39 rules extracted from goldens (independently verified), Phase 1 baselines collected, Phase 2 substrate proven (4→13 refinement recall, 3.25× improvement), Cluster A + Step 2 + Step 3 + F-D landed, Phase 3 integration sim composes cleanly, codex code-review findings addressed. Ready for fresh dogfood.

> **READ FIRST:** [plans/run-32-phase-3-preflight-status.md](run-32-phase-3-preflight-status.md) — what's actually committed, what's left, where to enter on next run.

**Audience:** Future agent picking up the recipe content-quality investigation. This doc replaces "edit briefs and re-sim" with a layered structural diagnosis. Read in full before proposing more brief edits.

**Replaces (in priority):** [run-32-content-quality-handoff-iter-2.md](run-32-content-quality-handoff-iter-2.md). The handoff documents what was tried; this doc says why it didn't work.

---

## TL;DR

After ~30 dogfood runs across run-21 through run-32 and a month of brief-iteration plateau, content quality stalls at "12 critical / 16 major / 7 minor defects vs human golden." The cause is **layered**, not single-cause. Brief edits operate at one layer (Layer 3); the highest-leverage open problems live at Layers 0–2.

Six layers, root → surface:

| # | Layer | Verdict | Tractability |
|---|---|---|---|
| 0 | **Target shape has no canonical exemplar.** All human goldens are single-codebase monoliths; the candidate is multi-codebase. The rubric anchors against shapes that don't represent the candidate's structure. | NEW finding | High — handcraft one multi-codebase exemplar; document the rules. |
| 1 | **No engine actor owns cross-codebase coherence.** Scaffold is per-codebase, content is per-codebase, refinement is whole-recipe-but-recognitional. S3 env-var names disagree across siblings, worker has dead DB wiring, KB headers diverge — all baked into the captured first-half output. | NEW finding | High — single new validator at scaffold complete-phase. |
| 2 | **Refinement is a pattern-matcher acting as a comparative reviewer.** Anchors are regex-shaped, framed as "polish where threshold holds", suspect pre-filter trains guided-mode behavior. Recognition is the bottleneck. | Confirmed | Medium — anchor reshape + framing change in atoms. |
| 3 | **Brief drowns its own content.** 105 KB total: 89% mechanics. Audience teaching IS present (briefs_content_phase.go:277-280 / 401-404 / 539 / 578-598); it's one paragraph in ~2500 lines. Prohibition-to-positive ratio 2.4:1. | Reframed | Medium — atom slim/cut, not atom-add. |
| 4 | **Brief leaks engine taxonomy.** 21-slug citation-guides bullet list at briefs_content_phase.go:1252-1274 lives verbatim in agent context, then prohibition rules ask agent not to surface it. Predictable verbalization-as-English. | Confirmed | Easy — strip the bullet list, replace with terse lookup hint. |
| 5 | **Facts are 28% contaminated; recordFact does zero sanitization.** 17/142 facts carry `zerops_dev_server`/`zsc noop`/"the agent" verbatim → direct passthrough to porter content. tier_decision Why often null. candidateSurface has 3 capitalization styles. | Confirmed | Easy — sanitize at Append() in facts.go:262-285. |

Brief edits we've done since run-21 sit entirely at Layer 3-4. They have moved the needle on individually-named defect patterns (the "wrong shape" the brief edit named) but never closed defect emergence rate, because Layers 0-2 keep feeding new failure shapes.

---

## Surprising findings (these contradict prior hypotheses)

### #1 — The brief's audience model is NOT implicit

Codex's first-pass diagnosis (#3 in its ranking) hypothesized adding an explicit audience paragraph would help. **Probe C disproved this.** [briefs_content_phase.go](../internal/recipe/briefs_content_phase.go) explicitly teaches audience at multiple points:

- **L277-280**: "You are the codebase-content sub-agent. Your job is to author the six surfaces this codebase ships..."
- **L401-404**: "IG #2-N owns porter-transferable mechanisms. A mechanism is a general rule the porter applies to their OWN code"
- **L539**: "Both reference recipes speak TO the porter, not AT them"
- **L578-598**: "Authoring-tool words leak agent perspective into porter content. The porter runs `npm`, `ssh`, `git` — never `zerops_dev_server` or 'the agent'"

The audience teaching exists. It is **drowning** in 40% synthesis_workflow + 33% facts + 14% citation/platform = ~89% of the brief is mechanics. One audience paragraph in 2500 lines exerts about as much pressure as one rule among hundreds.

**Implication:** "Add an audience paragraph" is not a fix. The fix is reducing the volume of what surrounds it.

### #2 — The brief HAS cross-codebase awareness

L119, L198-202, L1800-1810 explicitly tell the apidev agent that appdev and workerdev are dispatched in parallel and that managed-service findings cross-propagate via `CrossCodebaseManagedServiceFacts` (run-26 F-31). Cross-codebase awareness is not the missing piece. **The missing piece is an actor that enforces cross-codebase consistency.**

### #3 — First half is mostly producing porter-grade input

Probe F: source code is idiomatic NestJS/Vite/NestJS-standalone. Yaml field choices defensible. 9/10 sampled facts porter-actionable. Run-22's NATS drain bug IS fixed in code. **The user's "first half is working reasonably" presumption holds on the per-codebase axis.**

### #4 — But first half emits cross-codebase INCOHERENCE

- **apidev/workerdev disagree on S3 env-var names** ([apidev/zerops.yaml:102-103](../docs/zcprecipator3/simulations/32-pattern2-fix-1/apidev/zerops.yaml#L102-L103) uses `S3_ACCESS_KEY_ID/S3_SECRET_ACCESS_KEY`; [workerdev/zerops.yaml:75-76](../docs/zcprecipator3/simulations/32-pattern2-fix-1/workerdev/zerops.yaml#L75-L76) uses `S3_KEY/S3_SECRET`). Same managed service, two contracts. Porter inherits a broken cross-codebase library boundary.
- **workerdev wires DB_HOST/PORT/USER/PASS** but [app.module.ts](../docs/zcprecipator3/simulations/32-pattern2-fix-1/workerdev/src/app.module.ts) never imports TypeOrmModule. Dead env wiring.
- **KB headers diverge:** apidev = no header, workerdev = `## Knowledge base`, appdev = `### Gotchas`.

These cannot be fixed in content phase — content faithfully renders what was scaffolded. **Cross-codebase coherence is a class of defect with no engine actor responsible for it.**

### #5 — Cross-framework drift was already at scaffold time

[facts.jsonl line 90](../docs/zcprecipator3/runs/32/environments/facts.jsonl#L90) literally cites `Express/Fastify` as alternatives in the fact text recorded during scaffold. The "teaching kit" feel of published content is **inherited from contaminated facts**, not generated by content sub-agents. Content phase does what its input tells it; brief edits at content phase chase a downstream shadow.

### #6 — Refinement structurally CANNOT do what an external reviewer does

Probe A. [briefs_refinement.go:58-188](../internal/recipe/briefs_refinement.go#L58-L188) — anchors are pattern-shaped (literal phrase / shape regex / observable-state strings); facts are 80 KB-cap'd most-recent-first; suspect-list framing trains guided-mode; threshold flipped to ACT in run-23 but ACT-bias only matters once recognition fires.

**Recognition is the bottleneck.** The HOLD-vs-ACT debate was a red herring.

---

## Why refinement misses what humans + external judges catch

**User correction (load-bearing):** the fix is NOT "feed refinement the golden recipe". Goldens are not portable; per-recipe goldens don't scale. The substrate has to be **rules**, not exemplars.

What humans/external judges actually do when reviewing recipe content:
- Apply portable rules: "siblings agree on env-var conventions"; "recipe names what it IS, not what it could be"; "internal taxonomy doesn't surface in porter content"; "audience is the porter who clones-and-runs in 5 minutes".
- Walk every fragment with those rules in working memory.
- Cite the violated rule + the exact phrase + the surgical edit.

What refinement does today:
- Holds rubric criteria descriptions (mostly correct).
- Pattern-matches anchors against the engine-flagged suspect list.
- Defaults to ACT-bias (correct) once recognition fires; never fires for novel shapes.

The gap closure direction:
- **Principle-shaped anchors**, not regex anchors. Example: "Does this fragment teach a framework that isn't this recipe's?" — agent enumerates per-fragment.
- **Discovery-mode**, not guided-mode. Walk every fragment × every rule.
- **Audience-violation evidence preserved** in the facts cap (don't evict authoring-vocab signals via recency sort).
- **Framing as last gate**, not polish.
- **Add the cross-codebase-coherence rule set** to the rubric (currently absent because monolith goldens have no cross-codebase axis).

This is rule-substrate work, not exemplar-substrate work. Affects [embedded_rubric.md](../internal/recipe/content/briefs/refinement/embedded_rubric.md), [synthesis_workflow.md](../internal/recipe/content/briefs/refinement/synthesis_workflow.md), [phase_entry/refinement.md](../internal/recipe/content/phase_entry/refinement.md), and the suspect-collection logic.

---

## Forensic baseline (the data each layer rests on)

### Probe A — refinement gap

- Refinement does NOT see laravel-jetstream or any golden ([briefs_refinement.go:58-188](../internal/recipe/briefs_refinement.go#L58-L188)).
- Anchors classified: 3 sampled, all pattern-shaped (regex/literal phrase). Zero principle-shaped.
- Suspect list pulled from existing notices + cheap rubric regex pre-scan ([refinement_suspects.go:9-50](../internal/recipe/refinement_suspects.go#L9-L50)). Comment says "minimum, not exhaustive" but framing trains guided exploration.
- Threshold language at [phase_entry/refinement.md:28-53](../internal/recipe/content/phase_entry/refinement.md#L28-L53) — ACT-biased post run-23, with snapshot/restore rationale.
- Facts cap 80 KB most-recent-first ([briefs_refinement.go:155-180](../internal/recipe/briefs_refinement.go#L155-L180)) — evicts audience-violation evidence over time.
- Framed as "refine where the threshold holds" — narrow, polish-coded.

### Probe B — facts forensic on real run-32 data

- 142 records, 103 KB. Contamination 28% (40/142).
- 17 records carry `zerops_dev_server` / `zsc noop` / "the agent" verbatim. → direct leakage to [environments/1 — Remote (CDE)/import.yaml:23](../docs/zcprecipator3/simulations/32-pattern2-fix-1/environments/1%20—%20Remote%20%28CDE%29/import.yaml#L23).
- 20 records duplicate/near-duplicate. No cross-phase dedup.
- 10 tier_decision records have `Why: null`. [facts.go:107-129](../internal/recipe/facts.go#L107-L129) Validate() doesn't require Why.
- candidateSurface field has 3 capitalization styles in same run (`CODEBASE_KB` / `codebase-knowledge-base` / `CODEBASE_KNOWLEDGE_BASE`). Surface routing fights itself.
- 9/10 random facts porter-actionable on signal axis. Contamination is on token/voice axis, not topic axis.
- recordFact handler ([facts.go:262-285](../internal/recipe/facts.go#L262-L285)) does zero sanitization. Pure passthrough.
- **Missing fact kinds:** no negation-facts. No record for "worker doesn't talk to Postgres" → dead DB wiring is invisible. No record for "this is a NestJS-only recipe; do not name alternative frameworks". No record for "CLAUDE.md not deliverable".

### Probe C — brief composition end-to-end

- apidev brief: 105,213 bytes (105 KB).
- Section breakdown: synthesis_workflow 40.7% / facts 33.9% / citation+platform 14.5% / dispatch+context 9.6% / metadata 4.4%.
- 58 prohibition lines vs 24 positive examples. 2.4:1 prohibition-to-positive.
- Citation-guides slug list at [briefs_content_phase.go:1252-1274](../internal/recipe/briefs_content_phase.go#L1252-L1274) — 21 corpus slugs, backtick-bulleted, header is "Citation guides for this recipe" (looks like menu options, not lookup keys).
- Audience teaching: present at L277-280 / L401-404 / L539 / L578-598.
- Cross-codebase awareness: present at L119, L198-202, L1800-1810.
- Surface placement awareness: present at L121-125, L350-371, L838, L1149.

### Probe D — goldens audit

- 9 substantive recipes at [/Users/fxck/www/recipes/](../../recipes/), all single-codebase monoliths.
- zerops-showcase ([/Users/fxck/www/recipes/zerops-showcase/README.md](../../recipes/zerops-showcase/README.md)) describes "distributed" but is one repo with service folders.
- Root README size in goldens: 2.1–2.3 KB. 0 H2 sections. 80%+ links to zerops.io. 8-line tier intros. 33–36% comment density in zerops.yaml.
- Candidate root README: 1.9 KB (within bar). Per-codebase READMEs balloon (apidev 350+ lines).
- **No golden demonstrates a multi-codebase shape.** Multi-codebase = 0 reference exemplars.
- Goldens disagree on: comment voice (declarative vs directive), KB shape, framework-naming policy.

### Probe E — cross-codebase / placement awareness

- CC-content sub-agent KNOWS sibling exists (parallel claudemd-author) but does NOT receive sibling NAMES + ROLES + STRATEGIC SHAPE.
- Surface placement: agent knows fragment IDs but NOT how stitch composes them into the document hierarchy.
- Finalize sub-agent ([briefs.go:791-863](../internal/recipe/briefs.go#L791-L863)) gets a flat codebase symbol-table at L859-863 — zero framing on "your job for root README is to orient the porter to the 3-codebase architecture".
- ZERO engine pressure for cross-codebase secret-naming consistency at scaffold complete-phase.
- ZERO engine cross-codebase consistency check anywhere between scaffold and refinement.

### Probe F — first-half quality audit

- Per-codebase quality: idiomatic, minimal, would-keep. Source code is delete-and-replace scaffolding done well.
- Yaml field choices: defensible per-codebase.
- Run-22 NATS drain bug IS fixed in workerdev code.
- 9/10 sampled facts porter-actionable.
- **One critical defect: S3 key naming inconsistency** — apidev `S3_ACCESS_KEY_ID/S3_SECRET_ACCESS_KEY` vs workerdev `S3_KEY/S3_SECRET`. Same backend, different contracts.
- Worker dead DB wiring confirmed.

---

## Eval design — counters, not judges

LLM-judge-against-golden cannot reliably discriminate brief edits below ~0.5σ. Replace with mechanical counters that run on a captured second-half replay (cheap; no platform contact required):

| # | Counter | Implementation | Closes |
|---|---|---|---|
| 1 | Cross-codebase env-var coherence | regex over all `<host>dev/zerops.yaml` for `${<service>_*}` references; flag mismatched key names targeting same source | Defects #10, #19 |
| 2 | Slug-leakage in published markdown | regex over published md for backticked or English-cased corpus slugs in link text or body | Pattern #2 |
| 3 | Cross-framework verb count | regex for "Express\|Fastify\|Webpack\|Astro\|SvelteKit\|Next\.js" in IG bodies, weighted negative when not the candidate framework | Defect #18 |
| 4 | Authoring-token leak | regex for `zerops_dev_server\|zcli\|zsc noop\|the agent\|record-fact` in published content | Defect #12 |
| 5 | Fact contamination rate | regex applied to facts.jsonl Why/mechanism/fixApplied fields | Layer 5 root |
| 6 | tier_decision Why-fill rate | % of `kind=tier_decision` records with non-empty Why | Defect #13 |
| 7 | Refinement recall | dispatch captured run to (a) live refinement and (b) external-judge-with-rules; rate refinement's catch-rate | Layer 2 |
| 8 | KB-header consistency | regex for the H2/H3 used as KB section header per codebase; count distinct values | Defect #11 |

These are deterministic and fast. Run on captured `simulations/32-pattern2-fix-1/` baseline. Vary one thing → re-run sim from `emit` onward → measure delta. **No LLM in the metric loop.**

---

## Suggested attack order

Smallest blast radius first; only escalate if the prior level's counter delta is null.

| # | Action | Layer | Cost | Counter that should move |
|---|---|---|---|---|
| 1 | Sanitize facts at recordFact Append() — strip authoring-vocab tokens before write | 5 | One file, ~1 day | Counter #4, #5 |
| 2 | Cross-codebase coherence validator at scaffold complete-phase — flag env-var naming mismatches across siblings consuming same managed service | 1 | One validator, ~2 days | Counter #1 |
| 3 | Strip citation-guides bullet list from cc-content brief — replace with one-line "lookup keys for zerops_knowledge — never surface in porter content" | 4 | One atom edit, ~1 hour | Counter #2 |
| 4 | Trim cc-content brief — target ≤60 KB by collapsing 33% facts section to highest-signal subset + dedup + collapsing redundant prohibition pairs | 3 | Atom + composer change, ~1 week | Counter #2, #3 (audience signal stronger) |
| 5 | Reshape refinement anchors from regex to principle-form. Add cross-codebase-coherence rules. Reframe as "discovery mode, last gate". | 2 | Multi-atom + suspect logic, ~1-2 weeks | Counter #7 |
| 6 | Handcraft one multi-codebase reference — extract its rules into the rubric (NOT the recipe itself; rules are portable) | 0 | High-judgment work, ~2 weeks | All counters |
| 7 | Outline-then-write coordinator pass | 1+3 | Architectural; defer | Last-resort |

Step 1 + 2 + 3 are confidence-builders — small interventions with measurable deltas. They establish whether the eval harness itself works before paying for Step 4-6.

---

## Open questions for next instance

- **Counter calibration.** What's the baseline reading of each counter on the captured run-32 sim? Without baselines, deltas are uninterpretable. Probably the very first thing to do.
- **Step 1 boundary.** Sanitize at Append() = stripping. Refuse-on-Append() = harder line. Which? Affects whether contaminated facts are silently fixed or surface as scaffold-time errors.
- **Step 2 scope.** S3 keys are the smoking gun, but cross-codebase coherence has many axes (DB env wiring, service hostnames, port numbers, secret-name conventions). Does the validator try to be exhaustive or start narrow?
- **Step 6 boundary.** Is the multi-codebase reference a real recipe (publishable) or a synthetic rubric exemplar (rules-only, no actual code)? User's stated stance favors rules; revisit when reaching this step.
- **Refinement evidence cap.** Should the 80 KB cap sort by relevance instead of recency? Audience-violation evidence (authoring-vocab signals) lives in scaffold-phase facts that get evicted in long runs.

## What NOT to do (until counters move)

- Don't run another full dogfood. Replay second half from captured snapshot; measure counters; iterate at that layer.
- Don't add a new prohibition rule to a brief atom. Run-22 onward shows prohibitions don't compose into values; agents follow letter, miss intent.
- Don't add an audience-paragraph atom. Audience teaching is already at briefs_content_phase.go:277-280 et al; volume is the problem.
- Don't expand the rubric anchor set within the existing pattern-matching shape. Adding more regex anchors doesn't address the recognition-is-the-bottleneck finding.
- Don't paraphrase the codex first-pass diagnosis. It got #2 and #4 right (slug leakage; dispatch-shape note) and #1, #3 wrong (audience model is not implicit; positive examples are not the dominant pollution vector).

---

## Pointers for the next instance

1. **Read this doc.**
2. [run-32-content-quality-handoff-iter-2.md](run-32-content-quality-handoff-iter-2.md) — the iteration that surfaced the plateau; 19 head-to-head defect table.
3. [docs/zcprecipator3/system.md](../docs/zcprecipator3/system.md) §4 — TEACH/DISCOVER line; this diagnosis fits the catalog-drift signature at scale.
4. [docs/zcprecipator3/simulations/32-pattern2-fix-1/](../docs/zcprecipator3/simulations/32-pattern2-fix-1/) — captured second-half replay surface; all counters run against this.
5. Code anchors:
   - [internal/recipe/facts.go:262-285](../internal/recipe/facts.go#L262-L285) — Append() — Step 1 site
   - [internal/recipe/briefs_content_phase.go:1252-1274](../internal/recipe/briefs_content_phase.go#L1252-L1274) — citation-guides bullet list — Step 3 site
   - [internal/recipe/briefs_refinement.go:58-188](../internal/recipe/briefs_refinement.go#L58-L188) — refinement composer — Step 5 site
   - Validator add site for Step 2: TBD — no existing validator owns cross-codebase coherence. Add new file.
6. Sim re-run: `go build -o /tmp/zcp-recipe-sim ./cmd/zcp-recipe-sim && /tmp/zcp-recipe-sim emit -run docs/zcprecipator3/runs/32 -out docs/zcprecipator3/simulations/<N>` then walk through dispatch → stitch → emit-finalize → finalize agent → emit-refinement → refinement agent → stitch → validate.

---

## Verification triage — what's testable how

For each item in the attack order + each validation correction, classify as STATIC (read existing artifacts) / SIM (replay second-half from captured or modified-captured snapshot) / FRESH-RUN (agent must be in-loop for first-half).

### STATIC (no sim, no run)

| Item | Why static |
|---|---|
| Layer 0 — extract quality rules from goldens (single + multi-codebase) | Pure file reading. Compare candidate against extracted rules. |
| Layer 1 — cross-codebase coherence validator (DETECTION only) | Pure function over captured yamls. |
| Layer 5b — enum/schema drift catalog | Already done in validation pass. |
| Counter #1 baseline (env-var coherence) | Regex over captured yamls. |
| Counter #5 baseline (fact contamination rate) | Regex over captured facts.jsonl. |
| Counter #6 baseline (tier_decision Why-fill rate) | jq over facts.jsonl. |
| Counter #8 baseline (KB header consistency) | grep over published READMEs. |

### SIM (replay second-half from captured-or-modified snapshot)

| Item | What you change / replay |
|---|---|
| Step 1 (sanitize facts) — published-content effect | Pre-sanitize captured facts.jsonl as one-shot, replay. |
| Step 2 (cross-codebase coherence) — DOWNSTREAM effect of coherent input | Hand-edit captured yamls to align, replay. The "what would coherent first-half produce" what-if. |
| Step 3 (relabel citation-guides list) | Edit composer + atom, replay. |
| Step 4 (trim brief to ≤60 KB) | Edit synthesis_workflow.md atom + composer, replay. Test-pin breakage surfaces. |
| Step 5 (reshape refinement anchors → principle-form) | Edit embedded_rubric.md + refinement_suspects.go, replay refinement step only. Cheapest. |
| Counters #2/#3/#4/#8/#9 — under any second-half intervention | Static over post-sim published markdown. |
| Counter #7 (refinement recall) | Honest LLM-in-loop. Captured run vs external rule-judge. |
| Layer 2 reshape verification — does refinement catch more? | Replay refinement, measure recall delta. |

Sim cost is Claude API calls, not platform calls. ~10 sub-agent dispatches per replay.

### FRESH RUN (agent in-loop for first-half)

| Item | Why fresh |
|---|---|
| Step 2 — does validator PRESSURE produce coherent yamls at source? | Sim tests downstream effect; only fresh run tests agent self-correction. |
| Step 1 — does sanitization-at-Append change AGENT behavior? | Probably not, but only fresh confirms. |
| Step 7 (outline-then-write coordinator) | Architectural. |
| tier_decision.Why becomes load-bearing required field | Forces scaffold/feature agents to fill at record time. |
| Refusal-not-warn validator at scaffold complete-phase | Refusal is feedback loop with agent; sim can't simulate revision. |

### Practical sequencing

Static + sim covers ~80% of the diagnosis. Fresh-run-required items are mostly "does the agent self-correct under pressure" — only matters AFTER static + sim establish the intervention matters in principle.

Suggested order:
1. **Static baselines today.** Counters #1, #5, #6, #8 numbers + golden rule extraction. Half a day.
2. **Sim Step 5 first.** Refinement reshape — replay refinement step only, cheapest sim. Tests Layer 2.
3. **Sim Step 1 + 3 together.** Sanitize captured facts + relabel slug list, full second-half replay. Tests Layer 4 + 5a.
4. **Sim Step 2 with hand-edited yamls.** Coherent-input what-if. Tests whether content phase is structurally constrained by first-half coherence.
5. **Only after 1-4 — fresh run** to test agent self-correction under new validators.

---

## Validation status

- [x] Fresh-eyes Opus pass — done 2026-05-08
- [x] Codex independent pass — done 2026-05-08

Both passes pushed back. They converge on the following corrections — **read these before acting on the layer-by-layer claims above**.

### Validation correction #1 — Layer 0 is overstated (both passes)

`/Users/fxck/www/recipes/zerops-showcase/` IS a multi-codebase exemplar. [zerops-showcase/1 — Remote (CDE)/import.yaml:17,44](../../recipes/zerops-showcase/1%20—%20Remote%20%28CDE%29/import.yaml) declares two distinct `buildFromGit` repos (`showcase-recipe-app`, `showcase-recipe-worker`) plus shared managed services. README at line 4 reads "Bun + React frontend ... Python worker for async image processing". Probe D dismissed this as "one repo with service folders" — wrong.

Codex's broader sweep: of 48 recipes inspected, 5 have multiple distinct `buildFromGit` repos; weak coverage but non-zero. Multi-codebase reference exemplars exist; we just haven't extracted their rules.

**Implication for attack order:** Step 6 ("handcraft one multi-codebase exemplar") likely collapses to a 1-day rule-extraction task against zerops-showcase + the 4 other multi-buildFromGit recipes. Validate before paying multi-week handcraft cost.

### Validation correction #2 — Line citations in `briefs_content_phase.go` are wrong (both passes)

Every `briefs_content_phase.go` line number cited in the diagnosis is wrong. The file is 665 lines (not the 1252-1274 range cited). The actual citations:
- Audience teaching site: [internal/recipe/content/briefs/codebase-content/synthesis_workflow.md:1-6, 263-281, 304-325](../internal/recipe/content/briefs/codebase-content/synthesis_workflow.md) — it's an atom, not the composer.
- Citation-guides bullet emit: [briefs_content_phase.go:520-537](../internal/recipe/briefs_content_phase.go#L520-L537) (Codex) or [briefs_content_phase.go:532](../internal/recipe/briefs_content_phase.go#L532) (Opus). The "L1252-1274" range was the brief's RENDERED line numbers in `api-prompt.md`, not the source.
- Phase-entry also leaks taxonomy: [content/phase_entry/codebase-content.md:126-143](../internal/recipe/content/phase_entry/codebase-content.md#L126-L143) lists slugs.

**Substance of Layer 3 + 4 survives**; the citations were sloppy. Future agent following file:line refs will be confused — re-grep before acting.

### Validation correction #3 — Layer 2 ("all anchors are pattern-matchers") oversimplifies (both passes)

Sampling [embedded_rubric.md](../internal/recipe/content/briefs/refinement/embedded_rubric.md) shows mixed shape:
- Principle-shaped sections exist: trade-off two-sidedness ([:428-441](../internal/recipe/content/briefs/refinement/embedded_rubric.md#L428-L441)), classification routing ([:491-500](../internal/recipe/content/briefs/refinement/embedded_rubric.md#L491-L500)), cross-surface duplication topic-index ([:621-638](../internal/recipe/content/briefs/refinement/embedded_rubric.md#L621-L638)).
- Pattern-shaped sections also exist: [:99-107](../internal/recipe/content/briefs/refinement/embedded_rubric.md#L99-L107), [:202-227](../internal/recipe/content/briefs/refinement/embedded_rubric.md#L202-L227), [:340-372](../internal/recipe/content/briefs/refinement/embedded_rubric.md#L340-L372).

**More accurate framing:** principles exist in criterion narratives; the SCORING anchors are regex-collapsed. That's what trains pattern-matching behavior. The fix is reshape SCORING to match the principle, not "introduce principles" (they're already there).

### Validation correction #4 — Layer 5 should be split (Codex)

Two distinct failure modes are conflated:
- **Token contamination** (17 records carry `zerops_dev_server` etc.) — sanitization fix.
- **Schema/enum drift** — `candidateSurface` has **9** distinct spellings in run-32 facts.jsonl (not 3 as diagnosed): `CODEBASE_KB` / `CODEBASE_KNOWLEDGE_BASE` / `codebase-knowledge-base` / `CODEBASE_IG` / `codebase-integration-guide` / `CODEBASE_ZEROPS_COMMENTS` / `codebase-zerops-comments` / `codebase-zerops-yaml` / `ENV_IMPORT_COMMENTS`. Scaffold brief teaches lowercase form ([decision_recording_slim.md:71-77](../internal/recipe/content/briefs/scaffold/decision_recording_slim.md#L71-L77)); synthesis expects uppercase ([codebase-content/synthesis_workflow.md:563-564](../internal/recipe/content/briefs/codebase-content/synthesis_workflow.md#L563-L564)). Drift silently misdirects facts to wrong surfaces.

Schema-enum normalization should be **Layer 6** (separate from contamination) and is arguably the cheaper fix: closed-enum validation in `validatePorterChange`/`validateTierDecision` at [facts.go:150-162](../internal/recipe/facts.go#L150-L162). Refuses on Append rather than passing through.

### Validation correction #5 — Step 1 vs Step 2 priority should swap (both passes)

Sanitizing facts at `Append()` closes the token-axis only (Counters #4, #5). Cross-codebase coherence validator closes a CLASS of structural defect (S3 keys, dead env wiring, host-name disagreements). Higher impact, similar cost — `consumes_services.go` already parses yaml + has the host-token regex, so Step 2 is closer to 1 day than 2.

**Revised order:** Step 2 → Step 1 → Step 6-validate (does zerops-showcase already cover it?) → Step 3-relabel → Step 4 → Step 5.

### Validation correction #6 — Step 3 should RELABEL not STRIP (Codex)

Stripping the citation-guides bullet list trades slug-leakage for citation-discoverability cost (the prose section at [api-prompt.md:601-664](../docs/zcprecipator3/simulations/32-pattern2-fix-1/briefs/api-prompt.md) only names 7 slugs by token; the bullet list adds 15 more, including the managed-services-* family). **Relabel:** "Internal lookup keys for `zerops_knowledge` — NEVER surface these tokens in published content. Use English descriptions of the SUBJECT instead." Preserves discoverability, kills the menu-of-options framing.

### Validation correction #7 — Counter #7 IS LLM-in-loop (both passes)

Counter #7 ("dispatch captured run to live refinement and external-judge; rate refinement's catch-rate") requires two LLM passes. Diagnosis claimed "no LLM in the metric loop" — wrong. Either replace with a mechanical oracle (regex set codifying the human-flagged defect classes) or label honestly as LLM-assisted. Other 7 counters are mechanical.

### Validation correction #8 — Step 4 ("trim brief to ≤60 KB, 1 week") is multi-week (Opus)

[synthesis_workflow.md](../internal/recipe/content/briefs/codebase-content/synthesis_workflow.md) is 975 lines. Each section was added with a test pin. Removing 40 KB without breaking pinned tests requires per-section justification + test reauthor. Realistic cost: 2-3 weeks, not 1.

### Validation correction #9 — One missed counter (Opus)

Add: **slug-allowlist counter** — engine knows the corpus slug list ([citations.go:43](../internal/recipe/citations.go#L43)); flag any backticked token in published `.md` matching a known slug appearing outside an explicit citation slot. More precise than the regex in Counter #2.

### Net effect on the diagnosis

- Layer 0 priority drops sharply if zerops-showcase covers the structural shape.
- Layer 5 splits into two layers (5a token contamination, 5b enum/schema drift).
- Layer 2's "all pattern-shaped" claim weakens to "scoring anchors are pattern-shaped, principles exist in narratives but aren't load-bearing in scoring".
- Attack order: Step 2 → Step 1 → Step 6-validate → Step 3-relabel → Step 4 → Step 5.
- Counter #7 honest-labeling required.

The **structural argument** (layered failure modes; brief edits chase Layer 3 surface symptoms while Layers 0-2 keep feeding new failures) **survives validation**. The **specific numerics, file:line citations, and one Probe D fact** were sloppy and are corrected above.

### Pending correction #10 — Layer 0 framing (open)

User flagged: codebase count is structural shape, not a quality axis. Quality rules (audience model, surface contracts, voice, scope discipline, slug-leakage, KB shape, cross-framework drift) are codebase-count-agnostic. A 1-codebase Laravel recipe and a 3-codebase NestJS recipe should follow the same quality rules.

**Implication:** the "find a multi-codebase exemplar" hunt was a red herring. Rule extraction should pull from ALL good goldens (single-codebase included). laravel-jetstream is a valid source of quality rules even though the candidate has 3 codebases.

**Layer 0 should be reframed** from "target shape has no canonical exemplar" to something narrower — possibly "rules-as-substrate hasn't been extracted from goldens at all". Open: discuss before rewriting Layer 0.
