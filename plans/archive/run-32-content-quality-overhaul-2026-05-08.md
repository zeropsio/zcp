# Run-32 content-quality overhaul — calibration drift from human golden

Author: 2026-05-08. Sibling to `docs/zcprecipator3/runs/32/ANALYSIS.md` (which incorrectly verdicted Axis B at-bar; this plan supersedes that verdict on Axis B). Successor to the iteration-cost spec at `plans/run-32-iteration-cost-fixes-2026-05-08.md` (which addressed agent-side smartness gaps but not content-quality structural defects).

This is NOT a brief-text fix wave. It's a systemic rewrite of the content-quality calibration chain because **every layer biases toward the early-flow recipe shape over the human golden**.

## The chain of bias (5 layers, all amplifying)

```
    Human golden                Early-flow recipe              Run-31/32 output
    laravel-jetstream-app  vs   laravel-showcase-app    →     nestjs-showcase
    [the bar]                   [the floor]                    [the actual]

         │                                                          ▲
         │ chosen as PRIMARY in research doc?                       │
         ▼                                                          │
                                                                    │
    docs/zcprecipator3/content-research.md  ────►  bias to early-flow
         │
         ▼
    docs/spec-content-surfaces.md  ────►  inherits research bias
         │
         ▼
    internal/recipe/content/briefs/refinement/embedded_rubric.md  ────►  codifies bias as scoring criteria
         │
         ▼
    internal/recipe/content/briefs/codebase-content/synthesis_workflow.md  ────►  brief teaches biased criteria
         │
         ▼
    Agent produces output that PASSES every rubric criterion AND is below human golden
```

**The agent is not failing. The system is calibrated wrong.** The rubric scored run-32 at 8.5+ on every criterion that ran; the user's eyeball pass surfaced 7+ structural defects in 30 seconds; codex confirmed each. The criteria the rubric uses are the wrong criteria.

## Diagnosis: why refinement passed run-32

The refinement gate exists "specifically to make absolutely and 100% sure it doesn't pass in shit state". It has 6 criteria. Mapped against the **11 identified patterns** (7 from user eyeball pass + 4 from codex independent pass):

| # | Pattern | Run-32 evidence | Rubric criterion that should catch it | Why it didn't |
|---|---------|-----------------|--------------------------------------|---------------|
| 1 | Tier name-drops on codebase surfaces | 5 mentions in READMEs, 3 in yamls | none | No criterion for cross-surface scope leakage |
| 2 | "Guide on Zerops docs covers Y" formula × 25 | 25 instances vs golden 0 | Criterion 3 (Citation prose-level) | **Criterion 3 actively REQUIRES this pattern.** The 8.5 anchor at `embedded_rubric.md:252-264` is *"The Zerops `<guide>` reference covers per-deploy key shape and the in-script-guard pitfall."* The agent followed the rubric verbatim — 25 times. |
| 3 | KB as flat bullets (not H3 + callouts + shells) | 16 flat bullets across 3 codebases | Criterion 1 (Stem shape) | Criterion 1 only checks the STEM (`- **stem** —`); enforces flat-bullet shape by assuming it. Goldens use H3 sub-headings + paragraphs + `> [!CAUTION]` blocks; rubric has no provision for that shape. |
| 4 | yaml comment density 56-63% (golden 36%) | 145/242, 56/88, 89/157 lines | Criterion 2 (Voice) | Criterion 2 checks for friendly-authority phrasings. No criterion for density. |
| 5 | IG items are conventions ("Alias under your own keys") not platform-forced | 4 of 15 IG items across 3 codebases | Criterion 5 (Classification × routing) | The rubric routing table classifies "alias your own keys" as code-flavor scaffold-decision which IS allowed on IG. The table itself is the wrong scope rule. |
| 6 | Intro leaks recipe-internal wiring | "Mounts under /api; JWT-ready via JWT_SECRET" / "Owns the items schema, worker owns audit_log" | none | `synthesis_workflow.md:334-335` says "non-trivial refinement is rare here; usually HOLD." Refinement explicitly abstains from intro authoring. |
| 7 | Cross-language adapt-paths in NestJS recipe | Python uvicorn, Go http listener in NestJS IG #2 | none | `synthesis_workflow.md:540-559` worked example mixes Express + Go as "Other frameworks". |
| **8** | **FACTUAL BUG: static app imported as Node service** | `runs/32/environments/3 — Stage/import.yaml:37-40` declares app `type: nodejs@22 zeropsSetup: prod`; `runs/32/appdev/zerops.yaml:74-81` prod runtime is `base: static` | none | **Severity: HIGH (factuality, not style).** Golden at `recipes/laravel-jetstream/3 — Stage/import.yaml:10-15` matches `type: php-nginx@8.4` to `laravel-jetstream-app/zerops.yaml:11-18` runtime base. Env-import emission doesn't validate service type against codebase prod runtime base. |
| **9** | **Root tier-label drift (taxonomy)** | `runs/32/environments/README.md:13-14` ships marketing labels "Include Coding Agents" / "Include Cloud IDE" | none | Golden at `recipes/laravel-jetstream/README.md:16-17` uses canonical taxonomy "AI agent" / "Remote (CDE)". Root README authoring rewrites canonical environment names. |
| **10** | **IG lacks concrete repo-file anchors** | `runs/32/apidev/README.md:256-299` IG steps use generic prose snippets, no source-file links | none | Golden at `laravel-jetstream-app/README.md:208-216` IG points directly at `composer.json`, `config/jetstream.php`, `zerops.yaml`. IG rubric optimizes principle explanation, not "where exactly in this repo do I change it?" |
| **11** | **Missing concept-bridge section after IG** | `runs/32/apidev/README.md:324-328` jumps from IG end directly to KB markers | none | Golden at `laravel-jetstream-app/README.md:223-224` inserts `## Understand Zerops Core Concepts` with a framework tutorial link between IG and KB. Codebase README template omits the post-IG learning bridge. |

**Score: 10 of 11 patterns uncovered by rubric. 1 of 11 (Pattern #2) is actively required by rubric. 1 of 11 (Pattern #8) is a factuality bug — not a style/calibration defect — meaning the recipe ships technically wrong content.**

The refinement gate is doing exactly what it was designed to do. The design was wrong.

## Three structural root causes

### Root cause 1 — Research doc canonized the early-flow shape over the human golden

`docs/zcprecipator3/content-research.md`:

- **§1.2** explicitly declares: *"KB bullets in the canonical '**Topic** — explanation' shape. Run-14 follows this shape; jetstream uses inline narrative + headings instead. **The early-flow shape is the right one for KB.**"*
- **§1.3 universal #4** codifies the early-flow KB shape as universal across both references — but the doc itself acknowledged in §1.2 that jetstream uses a different shape.
- **§1.1 length-budgets table** baselines run-26 numbers against the early-flow recipe, not against the human golden.
- **§4 style rules** point at "inline doc links encouraged in zerops.yaml + IG; KB bullets cite `zerops_knowledge` guide names when topic is covered" — codifying topic-name citation, not URL hyperlinks.

The research doc made structural choices about which reference recipe defines the bar. It chose early-flow over human-golden on at least three patterns (KB shape, citation shape, yaml-comment scope). Those choices propagated through the spec, rubric, briefs.

### Root cause 2 — Spec inherits research bias

`docs/spec-content-surfaces.md`:

- **Line 216** codifies: *"Pattern: 'The `<guide-id>` guide covers `<basic mechanism>`; the application-specific corollary is …'"* — verbatim adoption of the topic-name pattern.
- **Zero** occurrences of "hyperlink", "URL", "inline link", "markdown link" in the entire spec.
- Surface 5 (KB) contract enforces the flat-bullet shape; doesn't allow for H3 sub-headings + multi-paragraph bodies + callout blocks.
- Surface 4 (IG) contract doesn't constrain to platform-forced scope; convention-style items pass.

The spec is the second layer of bias. Even if the research were corrected, the spec would still teach the wrong shape.

### Root cause 3 — Rubric encodes the biased shapes as scoring criteria

`internal/recipe/content/briefs/refinement/embedded_rubric.md`:

- **Criterion 3 (Citation prose-level) requires the topic-name hand-wave formula.** 8.5 anchor: *"The Zerops `init-commands` reference covers per-deploy key shape and the in-script-guard pitfall."* Agents producing 25 instances of this pattern are scoring 8.5 by design.
- **Criterion 1 (Stem shape)** assumes flat-bullet shape via "Walk every KB bullet stem (the text between `**...**`)". Doesn't recognize H3-shaped KB.
- **Six criteria total. Five user-pattern uncovered. One pattern actively required by the rubric.**

This is why refinement passes content the user calls "absolute shit". The rubric's calibration target is wrong.

## What "intelligence not regex" means for the harness

The user explicitly rejected pattern-grep scoring: *"scoring harness won't ever work, we need intelligence, not comparing to hardcoded words and lines of comments"*. They are right ABOUT THE CALIBRATION-DRIFT PATTERNS. The 11 patterns we surfaced from eyeball reads (7 user + 4 codex) are NOT the entire defect surface — they're the visible top. Hardcoded patterns will catch known defects, miss the next wave, and accumulate as catalog-drift (system.md §4 forbids exactly this shape).

**However** (codex round-1 pushback): hard count invariants ARE the right place for grep. Line caps, item caps, byte caps live as count predicates already at `embedded_rubric.md:640-648` (tier-promotion guard) and `:670-676` (Unicode guard). Numeric thresholds aren't intelligence-shaped; they're count-shaped. The right architecture is **hybrid**:

- **Grep / count predicates** for: line caps, item caps, byte caps, occurrence counts of forbidden tokens IF the token is structural (like "guide on Zerops docs covers" — codified pattern, not natural prose), service-type-vs-runtime-base congruence checks, root-label-fidelity checks against canonical taxonomy strings.
- **LLM-as-judge** for: voice quality, mechanism-vs-decoration distinction, scope-leak detection, structural-shape evaluation (KB H3 vs flat), narrative-coherence between surfaces, novel-defect discovery.

The right harness:

```
[brief edit]
    │
    ▼
zcp-recipe-sim emit -run runs/32 -out /tmp/sim-N
    │
    ▼
[automated agent dispatch — N+1 sub-agents]
    │
    ▼
zcp-recipe-sim stitch + validate
    │
    ▼
LLM-as-judge eval:
   inputs:
   - candidate fragments at /tmp/sim-N/<host>{README,zerops.yaml,CLAUDE.md}
   - golden references: laravel-jetstream-app/* + recipes/laravel-jetstream/*/{README,import.yaml}
   - the 6 existing rubric criteria + the new criteria covering 7+ patterns
   - explicit instruction: "find content-quality defects. don't grep — read."
   output:
   - per-surface scores against rubric criteria
   - per-defect file:line evidence + golden-comparison snippet
   - regression flag: "this defect was present in run-32 baseline; new edit didn't fix it"
   - novel-defect flag: "this defect is NEW; not in run-32 baseline"
    │
    ▼
[if score below threshold OR new defect: iterate brief edit]
[if score above threshold AND known defects fixed: confirmed; lock as regression]
```

LLM-as-judge runs **codex (or Claude as backup)** with the corrected rubric as the scoring contract. The "find patterns I missed" pass against goldens IS the eval — it discovers new defects per iteration, doesn't need a pre-cataloged list.

## The plan — 7 phases (with codex-revised ordering)

Per codex round-1 review: Phases 1-4 (research/spec/rubric/brief rewrites) DON'T depend on sim alignment. They can run in parallel with Phase 0. Original "Phase 0 first" ordering was wrong. Also added **Phase -1 pilot** to de-risk before committing 12-17 days.

### Phase -1 — De-risk pilot (3-4 days)

Run a small focused experiment BEFORE committing to the full 7-phase plan. Per codex round-1 round-D pushback: the 12-17 day plan is too big to commit without measuring defect-capture rate of the new rubric criteria first.

- **Pilot scope:** root README + appdev codebase + Stage tier import.yaml — covers root taxonomy (#9), IG/KB shape (#3, #5, #10, #11), static-runtime mismatch (#8), env content + intro (#6), tier-label leakage (#1).
- **Pilot deliverable:**
  - First-pass rubric extension covering **criteria 7-16** (codex round-2 hard blocker fix: pilot exercises patterns #8-#11 which require criteria 14-16; original draft stopped at criterion 13 and would have produced false confidence). Anchored to golden file:line evidence.
  - Codex + Claude as parallel LLM-as-judge against the pilot scope.
  - Measure: do the new criteria catch the 6+ pilot-relevant patterns? At what false-positive rate?
- **Decision gate:** if defect-capture rate ≥80% on pilot AND false-positive rate ≤10%, commit to full plan. If lower, refine criteria before scaling. If much lower (<50%), the rubric architecture itself may be wrong; revisit framing.

### Phase 0 — Sim emit multi-file alignment (1 day, engine change; runs IN PARALLEL with Phases 1-4)

- **Why parallel:** sim's current single-file inline emit doesn't match production multi-file shape (production v9.73.0 ships multi-file `index.md + part-*.md` for codebase-content + env-content; sim's emit still uses legacy `BuildSubagentPromptForReplay` single-file path). User explicitly asked for byte-comparability against `runs/32/environments/.briefs/`. But this doesn't BLOCK doc/spec/rubric work — only blocks Phase 5 sim-driven harness.
- **Scope:**
  - Add `BuildCodebaseContentBriefMultiFile` + `BuildEnvContentBriefMultiFile` wrappers to `internal/recipe/briefs_content_phase_multifile.go` (refinement's `BuildRefinementBriefMultiFile` is the model).
  - Update `cmd/zcp-recipe-sim/emit.go::runEmit` to use them; produce `<out>/.briefs/<dispatch>/{index.md, part-*.md}` matching production layout.
  - **Codex round-1 fix + round-2 strengthening + round-3 boundary tightening:** define explicit normalization contract for byte-compare in BOTH the plan AND the test. Specifically the test must:
    1. **Strip the `<replay-adapter>` block** — exact pattern: lines from a line matching `^<replay-adapter>$` through (and including) the next line matching `^</replay-adapter>$`. If neither marker appears, no strip happens (production has no replay-adapter).
    2. **Strip the dispatch-pointer wrapper** — codex round-3 boundary fix: define this as the contiguous `## Dispatch — multi-file pointer` H2 section in sim's prompt, ending at the next `^## ` H2 heading OR end-of-file. Production's index.md does not carry this H2 because production's index.md IS the dispatch pointer; sim wraps it for replay-adapter context. After stripping, sim's prompt should start with the same `# Engine brief — codebase-content` heading production's index.md uses.
    3. **Path normalization (round-3 narrowed scope):** resolve only paths matching `^/.+\.briefs/<dispatch-name>/(index|part-\d+-[\w-]+)\.md$` to a relative form `<dispatch-name>/<filename>`. Other absolute paths (e.g. `/var/www/zcprecipator/<slug>/apidev/src/main.ts` references inside the brief body) MUST NOT be path-normalized — they're meaningful content differences if they diverge.
    4. **Compare** the normalized index.md byte-for-byte; compare each part-N file byte-for-byte after the same path-normalization.
    5. **Test FAILS** if any part is missing on either side OR if any part's body differs after normalization OR if either normalization rule above fails to produce equivalent output (e.g., sim has no `</replay-adapter>` close marker — that's a sim-emit bug).
  - Pin: `TestSimEmit_MultiFileShapeMatchesProduction` runs the normalization function (exposed for test reuse) before comparing.
- **Deliverable:** `diff -r <(normalize /tmp/sim-r32/.briefs) <(normalize runs/32/environments/.briefs)` returns empty.

### Phase 1 — Re-derive content-research from human golden (2-3 days, doc rewrite)

- **Why:** the research doc is the calibration source-of-truth; spec + rubric + briefs all derive from it. Fixing downstream first leaks bias upstream.
- **Scope:**
  - Re-do §1.2 ("two references aren't equal") with human-golden as PRIMARY bar (not equal-weight). Per codex caveat: early-flow remains useful for mechanism density + tradeoff naming (§1.2 lines 70-73), NOT as a structural template.
  - Re-do §1.1 length-budgets table; measure both references AND every run-26-32 output; explicitly mark which reference each cap derives from.
  - Add §1.4 — patterns that goldens have and run-31/32 lack:
    - H3 sub-headings in KB (#3)
    - callout blocks (`> [!CAUTION]`) (#3)
    - real URL hyperlinks vs topic-name hand-waves (#2)
    - "Production vs Development" section + recipe-features bulleted summary
    - embedded shell examples
    - **`## Understand Zerops Core Concepts` concept-bridge between IG and KB (#11, codex-found)**
    - **IG with concrete repo-file anchors (`composer.json`, `config/jetstream.php`, etc.) (#10, codex-found)**
    - **canonical environment taxonomy ("AI agent" / "Remote (CDE)" not marketing labels) (#9, codex-found)**
    - **service `type` in import.yaml matches codebase prod runtime base (#8, codex-found)**
  - Add §1.5 — patterns that run-31/32 have and goldens lack: tier name-drops (#1), doc-redirect formula (#2), cross-language adapt-paths (#7), intro recipe-internal wiring (#6), yaml comment over-density (#4), generic-prose IG without repo anchors (#10, codex-found).
  - Re-derive Part 5 routing tree to enforce platform-forced-only IG scope (#5).
  - Add **Part 6** — service-type-vs-runtime-base congruence rules (#8, codex-found): import.yaml service `type` MUST match the prod-setup `run.base` of the corresponding codebase. This is FACTUALITY, not style.
- **Deliverable:** updated `docs/zcprecipator3/content-research.md` with explicit human-golden primary calibration; codex review confirms zero remaining early-flow biases AND captures all 11 patterns.

### Phase 2 — Update spec-content-surfaces.md (1-2 days)

- **Why:** spec is the second layer; rubric reads from spec.
- **Scope:**
  - Replace topic-name citation pattern with hyperlink-or-complete-locally rule. Worked examples from goldens (`[Laravel documentation](https://laravel.com/...)`).
  - Update Surface 5 (KB) contract to allow H3 sub-headings + multi-paragraph + callout blocks + shell examples; flat-bullet remains valid as one option among several.
  - Update Surface 4 (IG) contract:
    - Enforce platform-forced-only scope (#5).
    - Anti-patterns: "alias under your own keys" — convention not IG; cross-language adapt-paths (#7) — wrong scope.
    - **Add concrete repo-file anchor rule (#10, codex-found):** IG items MUST link to the source files they apply to (e.g. `composer.json`, `config/jetstream.php`, `vite.config.ts`); generic prose without anchors is below-bar.
  - Update intro contracts (Surface 4 codebase intro, Surface 1 root intro): "describes the standalone app, not its deployment wiring" (#6). Anti-patterns: mount path, env-var alias, port number, inter-codebase coordination, schema-ownership.
  - Add yaml comment density target to Surface 7: aim for ~35% comment density (#4); comment the non-obvious; avoid defending decisions the code makes obvious.
  - **Add canonical-environment-taxonomy section to Surface 1 (#9, codex-found):** root README MUST use canonical environment names ("AI agent", "Remote (CDE)", "Local", "Stage", "Small Production", "Highly-available Production") — not marketing rewrites.
  - **Add post-IG concept-bridge section to Surface 4 (#11, codex-found):** between IG and KB sections, recipes SHOULD include `## Understand Zerops Core Concepts` linking to the framework-specific Zerops tutorial.
  - **Add factuality contract for Surface 3 import.yaml (#8, codex-found):** service `type` field MUST be congruent with the corresponding codebase's prod `run.base`. Mismatch is a factuality bug, not a style choice.
- **Deliverable:** updated `docs/spec-content-surfaces.md`; codex review confirms all 11 patterns have spec coverage.

### Phase 3 — Extend refinement rubric with new criteria (3-4 days)

- **Why:** the rubric is what refinement actually scores against. Without rubric extension, refinement passes the same defects.
- **Scope — 11 criteria additions/rewrites, mapped to all 11 patterns:**

| New/Updated Criterion | Targets | Scoring shape |
|----------------------|---------|---------------|
| **Criterion 3 (REWRITE) — Citation shape** | #2 (doc-redirect formula) | Hybrid: grep count of "guide on Zerops docs covers" formula = grep predicate (count > 5 = below-bar); LLM-as-judge for hyperlink-vs-complete-locally judgment |
| **Criterion 7 — Intro scope** | #6 (intro leaks wiring); intro-scoped subset of #1 (tier-drops on intros — redirected here, NOT scored under Criterion 12, see disambiguation note below) | LLM-as-judge: does the intro describe the standalone app, or does it leak deployment wiring (mount path, env-var aliases, port numbers, inter-codebase coordination, tier vocabulary)? |
| **Criterion 8 — yaml comment density target** | #4 (yaml over-density) | Grep count: comment-line / total-line ratio; flag if >50%; target 30-40% |
| **Criterion 9 — KB structural shape** | #3 (KB flat bullets vs H3 + callouts) | LLM-as-judge: does KB use the right shape for the substance? One-liner traps → flat bullets OK; multi-paragraph mechanism + porter recovery → H3 sub-heading + callout shape required |
| **Criterion 10 — IG scope (platform-forced only)** | #5 (alias-under-your-own-keys) | LLM-as-judge: is each IG item a thing the platform requires, or a code/config convention the porter could ignore? |
| **Criterion 11 — Adapt-path scope** | #7 (Python in NestJS) | LLM-as-judge: do adapt-paths stay within the codebase's language family? NestJS recipe → Node frameworks only; Laravel recipe → PHP only; etc. |
| **Criterion 12 — Tier scope (non-intro surfaces)** | #1 (tier name-drops on codebase surfaces, EXCLUDING intros which are scored under Criterion 7) | Grep count: occurrences of "tier [0-5]" / "Tier [0-5]" / "HA tier" / "stage tier" on codebase README BODY + zerops.yaml comments. Threshold: codebase surfaces should have 0; tier vocabulary belongs to env-content surfaces only. **Disambiguation rule (codex round-3 fix):** lines BETWEEN `<!-- #ZEROPS_EXTRACT_START:intro# -->` AND `<!-- #ZEROPS_EXTRACT_END:intro# -->` (both markers required to define the extent) are excluded from Criterion 12's scope — they're scored under Criterion 7. Lines outside that span are scored under Criterion 12. Prevents double-counting of tier mentions inside codebase intros AND prevents gap when an intro is missing one of the two markers (in which case the entire file falls into Criterion 12 scope and Criterion 7 reports the missing marker as a separate defect). |
| **Criterion 13 — Service-type/runtime-base congruence (FACTUALITY)** | #8 (codex-found, factual bug) | Grep predicate: parse import.yaml service `type` field; parse codebase prod-setup `run.base`; flag mismatch. HIGH severity — factuality bug, not style. |
| **Criterion 14 — Root README taxonomy fidelity** | #9 (codex-found) | Grep predicate: root tier links must use canonical names ("AI agent" / "Remote (CDE)" / "Local" / "Stage" / "Small Production" / "Highly-available Production"). Non-canonical labels = below-bar. |
| **Criterion 15 — IG concrete repo-file anchors** | #10 (codex-found) | LLM-as-judge: do IG items link to source files (`composer.json`, `config/...`, `vite.config.ts`) the porter edits? Generic prose without anchors = below-bar. |
| **Criterion 16 — Concept-bridge presence + content quality** | #11 (codex-found) | **Hybrid (codex round-2 fix + round-3 aggregation):** grep predicate for PRESENCE of `## Understand Zerops Core Concepts` heading between IG and KB; LLM-as-judge for CONTENT QUALITY of the bridge (does it link to a real framework Zerops tutorial; does the prose carry porter-relevant context, not just a stamped header). **Aggregation rule (codex round-3):** if grep half fails (heading absent), LLM half is N/A — defect counts ONCE as "absent". If grep passes but LLM judges low quality, defect counts ONCE as "low-quality". The LLM half only runs when the heading is present. Prevents double-counting of the same defect. |

- **Critical discipline:** anchor every new criterion to **golden behavior**, not a regex. 7.0 anchor = run-32 instance. 8.5 anchor = early-flow instance OR good run-32 instance. 9.0 anchor = human-golden instance.
- **Hybrid scoring (codex round-1 + round-2):** count predicates where the failure is count-shaped (#2 formula occurrences, #4 density ratio, #8 type-base mismatch, #9 canonical labels, #11 bridge presence (heading), #12 tier-mention count on non-intro surfaces); LLM-as-judge where the failure is shape/scope-shaped (#1 placement context, #3 structural fit, #5 platform-forced reasoning, #6 wiring scope, #7 language family, #10 anchor presence, #11 bridge content quality).
- **Codex round-2: existing rubric criteria 1-2, 4-6 also need explicit grep/LLM scoring assignment in this Phase 3 deliverable** — currently each criterion has anchor examples but no scoring-shape designation. As part of rubric extension, add scoring-shape per existing criterion (Criterion 1 stem shape — grep regex against signal classes already exists; Criterion 2 voice — LLM-as-judge for friendly-authority phrasings; etc.).
- **Deliverable:** updated `embedded_rubric.md`; codex review confirms each new criterion has anchors traceable to golden file:line evidence AND each scoring shape (grep vs LLM-judge) is justified.

### Phase 4 — Update synthesis_workflow + atom briefs + emitters + templates (3-4 days)

- **Why:** rubric is what refinement checks; synthesis_workflow is what every codebase-content + env-content sub-agent reads at AUTHORING time. Both need updates to consistently teach the corrected shapes. **Codex round-1: also update root/env templates AND any emitter guidance that chooses service `type` for import.yaml** (because pattern #8 is a factuality bug originating in the emit path).
- **Scope:**
  - Update `briefs/codebase-content/synthesis_workflow.md`:
    - Step 2 (IG): platform-forced-only test (#5), same-language adapt-paths (#7), hyperlink-or-complete-locally citation rule (#2), concrete repo-file anchors required (#10).
    - Step 3 (KB): both flat-bullet AND H3-structured shapes valid (#3); pick based on substance (one-liner trap → flat bullet; multi-paragraph mechanism + porter-recovery → H3 + callout).
    - Intro authoring (#6): "describes the standalone app, not deployment wiring" rule; explicit anti-pattern list (mount path, env-var alias, port number, inter-codebase coordination, schema-ownership).
    - yaml-comment authoring atom (#4): 35% density target; "comment the non-obvious" rule.
    - Step 1 (cross-surface scope) (#1): "no tier mentions on codebase surfaces; tier vocabulary belongs to env-content's per-tier authoring".
    - **NEW: post-IG concept-bridge teaching (#11)** — "after the last IG item, include `## Understand Zerops Core Concepts` linking to the framework tutorial".
  - Update `briefs/env-content/per_tier_authoring.md`:
    - **NEW: canonical taxonomy rule (#9)** — root README + tier README use canonical names "AI agent", "Remote (CDE)", "Local", "Stage", "Small Production", "Highly-available Production".
  - **NEW: update emitter guidance for import.yaml service `type` (#8, codex-found)** — `internal/recipe/yaml_emitter.go` (or wherever import.yaml service-type is set) MUST derive type from corresponding codebase prod `run.base`. This is engine-side; not a brief edit.
  - **NEW: update root README + env README templates (#9, codex-found)** — `internal/recipe/content/templates/*.tmpl` carry canonical taxonomy verbatim.
  - Update `briefs/refinement/synthesis_workflow.md` to add ACT/HOLD rules for criteria 7-16.
- **Deliverable:** every brief atom + emitter + template that teaches a content shape is consistent with the corrected rubric + spec; codex review confirms all 11 patterns have authoring-time teaching AND emit-time enforcement where applicable.

### Phase 5 — Hybrid scoring harness + sim-driven validation (2 days harness, 2-3 days first iteration)

- **Why:** sim emit + agent dispatch + hybrid grep+LLM scoring gives us a 10-15-min iteration loop vs. a 131-min dogfood. Brief edits A/B in parallel; novel defects discovered per iteration via "find patterns I missed" judge prompt.
- **Scope (codex round-1 reshape):**
  - Build sim driver script: `scripts/sim-content-quality.sh <slug>` that runs emit → automated dispatch → stitch → validate → hybrid scoring.
  - **Grep-predicate scorer** for count-shaped criteria: 8 (yaml density), 12 (tier mentions on non-intro surfaces), 13 (type/base congruence), 14 (canonical taxonomy), 16 (concept-bridge HEADING presence — grep half), parts of 3 (formula count).
  - **LLM-as-judge** for shape/scope-shaped criteria: 7 (intro scope, including tier-leakage in intros — disambiguated from #12), 9 (KB structural shape), 10 (IG scope), 11 (adapt-path scope), 15 (IG anchors), 16 (concept-bridge CONTENT QUALITY — LLM half), parts of 3 (hyperlink-vs-complete-locally judgment).
  - **Existing criteria 1-6** retain their current scoring (1 grep regex; 2 LLM-as-judge for friendly-authority; 3 hybrid post-rewrite; 4 LLM-as-judge for trade-off two-sidedness; 5 grep predicate against routing table; 6 LLM-as-judge for cross-surface dedup).
  - **Run BOTH codex AND Claude as parallel judges** (codex round-1: not "or backup"). Disagreement is signal — flag for human review.
  - **Novel-defect contract (codex round-1):** every flagged novel defect MUST carry: (a) candidate file:line, (b) golden counter-line file:line, (c) violated rubric criterion OR explicit "no rule covers this; question for review", (d) judge confidence (high/medium/low). Without all 4, label as "question" not "regression". Filters out hallucination.
  - Regression suite: locks in run-32 baseline scores per criterion; new sim runs flag regressions only against criteria with grounded evidence.
  - Run iteration N=3 against the corrected briefs to confirm all 11 patterns drop into golden-bar range.
- **Deliverable:** harness script + 3-iteration sim-confirmed brief wave + regression-locked baseline + judge-disagreement triage log.

### Phase 6 — Dogfood validation (run-33)

- Single fresh dogfood run-33 against the corrected briefs.
- Score with the same LLM-as-judge harness against goldens.
- Compare: run-32 baseline vs corrected-briefs sim N=3 vs run-33 dogfood. The three should converge.
- If convergence holds → declare F-track at-bar against human golden, archive run-32 ANALYSIS as superseded, update content-research.md run-32/run-33 baseline numbers.
- If convergence breaks → root-cause the dogfood-vs-sim divergence (likely real-platform behavior the sim doesn't replay).

## Validation strategy

Three external verifications gate this plan:

1. **Codex review of THIS plan** before any phase starts. Specifically: "find patterns I missed" — give codex the goldens + run-32 output + research/spec/rubric source-of-truth, ask codex to discover defects beyond the 7 listed here. Add to Phase 3 criterion list before rubric rewrite.
2. **Codex review of each phase's deliverable** before merging. Phase 1 (research doc): "is the human-golden bias actually corrected?" Phase 3 (rubric): "do the new criteria each have golden file:line anchors?" Phase 4 (briefs): "do briefs and rubric consistently teach the same shapes?"
3. **Sim-driven + dogfood-driven content scores** in Phase 5/6: numerical evidence that the corrected stack lifts content quality vs run-32 baseline.

## What this plan does NOT do

- It does NOT fix every defect in run-31/32. It builds the system that catches and fixes them through iteration.
- It does NOT add validators (regex-style) for the 7 patterns. Per system.md §4 catalog-drift, regex-ban-lists are wrong-side. The fix is positive TEACH-side rules + judgment-based DISCOVER-side scoring.
- It does NOT replace the iteration-cost spec we just shipped (F-47 through F-54). That spec addresses agent-side smartness gaps; this plan addresses content-quality structural defects. They're orthogonal.
- It does NOT promise the sim alone is sufficient. Phase 6 is dogfood validation; sim is the cheap iteration tool, not the verdict.

## Estimate

Phase -1 (de-risk pilot): 3-4 days
Phase 0 (sim alignment, parallel): 1 day
Phase 1 (research rewrite): 2-3 days
Phase 2 (spec update): 1-2 days
Phase 3 (rubric extension): 3-4 days
Phase 4 (briefs + emitters + templates): 3-4 days
Phase 5 (hybrid harness + 3 iterations): 4-5 days
Phase 6 (dogfood validation): 0.5-1 day

**Total: ~17-23 days**, with codex validation gating each phase. Phase -1 pilot determines whether to commit to full plan or refine criteria first.

This is a content-quality stack rewrite. The output is durable (the calibration chain matches human golden permanently) but the upfront cost is ~3-4 weeks. Alternative is iterating on the broken calibration chain with iteration-cost-style fix waves indefinitely; net cost is higher.

## Codex review log

**Round 1 (this revision):**
- 4 NEW patterns surfaced (#8 factuality bug, #9 root taxonomy, #10 IG anchors, #11 concept bridge). Plan updated; rubric criteria 13-16 added.
- Phase 0 byte-compare normalization underspecified — explicit normalization contract required.
- Hybrid grep+LLM scoring required (codex round-1 §B); pure LLM-as-judge was wrong. Plan updated.
- Phases 1-4 don't depend on Phase 0; can run parallel. Plan re-ordered.
- 12-17 day commit too big without de-risking pilot. Phase -1 added (3-4 day pilot + decision gate).
- Novel-defect contract needed strict grounding (candidate line + golden counter-line + rule + confidence). Plan updated.
- LLM-as-judge: BOTH codex AND Claude in parallel (not "or backup"); disagreement is signal.

**Round 2 (this revision):**
- 1 hard blocker fixed: Phase -1 deliverable extended to criteria 7-16 (was 7-13); pilot now covers the full set of new patterns it claims to exercise (#8-#11 require criteria 14-16).
- 2 newly-introduced issues fixed:
  - Criterion 7 vs Criterion 12 double-counting on tier mentions in intros — disambiguation rule added: intros scored by C7 only; C12 explicitly excludes intro extract markers.
  - Criterion 16 (concept bridge) now hybrid (grep for heading presence + LLM for content quality); Phase 5 harness updated.
- 2 watch items addressed:
  - Phase 0 normalization contract spelled out in plan (5 explicit substitution rules, not just "in the test").
  - Existing rubric criteria 1-2, 4-6 now have explicit grep/LLM scoring assignments.

**Round 3 (this revision):**
- Codex found 3 boundary/aggregation issues introduced by round-2 fixes:
  - C7 vs C12 disambiguation named only the START marker; END marker was missing — fixed by requiring both markers + explicit "missing-marker → entire file falls into C12 + C7 reports the marker absence as a separate defect" rule.
  - C16 hybrid scoring had no aggregation rule — fixed: grep half failure (heading absent) makes LLM half N/A; defect counts ONCE not TWICE.
  - Phase 0 normalization rule 2 (dispatch-pointer wrapper) end boundary was undefined — fixed with explicit "next `^## ` H2 heading OR end-of-file" terminator. Rule 3 (path resolution) was over-broad — fixed by narrowing scope to ONLY `.briefs/<dispatch>/` part paths via regex; other absolute paths are meaningful content and must NOT be normalized.

**Round 4 (next):** validate the revised plan is ready to start Phase -1.

## Out of scope

- Engine-side validator additions (regex-style ban-lists per system.md §4 — wrong shape, would be catalog-drift)
- Replacing zcprecipator3 with a different recipe-authoring approach (much bigger scope)
- Goldens curation expansion beyond `laravel-jetstream-app` + `recipes/laravel-jetstream/*` (could be a follow-up if the corrected stack reveals goldens calibration gaps)
