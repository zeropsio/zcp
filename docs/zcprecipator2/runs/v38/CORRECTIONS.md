# runs/v38/CORRECTIONS.md — what the first v38 analysis missed

**Context**: v38 analysis first shipped as commit `e668db2` "v38 run analysis + verdict — PAUSE" on 2026-04-22. The verdict direction (PAUSE) was correct; the reasoning was incomplete. The user pushed back — "from your verdict I feel like you want to add stripping, instead of addressing why the wrong content got there in the first place" — and pointed at [`docs/spec-content-surfaces.md`](../../spec-content-surfaces.md). That question reopened the analysis. This document lists what the first pass missed + why + what the corrected picture looks like.

The companion [`verdict.md`](verdict.md) stands on the evidence it cites, but its §4 "new findings" and §7 "what v39 needs" are superseded by the findings below. The checklist's retry-cycle-18 row ("editorial-review returned clean") is factually wrong and is superseded by §2 of this document.

---

## 1. The missed finding — Go-source content authorship has no quality gate

**What the first pass wrote**: "Writer-authored per-codebase content is production-grade" + "Env READMEs + import.yaml comments are high quality (F-21 correction applied after cycle 17)" — all `pass` cells in the content-quality section of the checklist.

**What the first pass should have surfaced**: the env-tier README prose is NOT writer-authored. It is hardcoded in Go at [`internal/workflow/recipe_templates.go`](../../../internal/workflow/recipe_templates.go) across four functions:

| Function | Lines | What it emits |
|---|---|---|
| `envAudience(envIndex)` | L220-262 | "Who this is for" bullets, indexed by envIndex (0-5) |
| `envDiffFromPrevious(envIndex)` | L269-309 | "What changes vs adjacent tier" bullets, indexed by envIndex |
| `envPromotionPath(envIndex)` | L314-352 | "Promoting to next tier" bullets, indexed by envIndex |
| `envOperationalConcerns(envIndex)` | L357-403 | "Tier-specific operational concerns" bullets, indexed by envIndex |

Every one of these `switch envIndex` blocks returns a hardcoded string literal. None of them take `plan` parameters that would let the prose reflect the actual import.yaml configuration for that tier — the ONLY plan-derived interpolation on env READMEs is framework title, slug, and pretty-name (via `GenerateEnvREADME` at L155-213).

**Consequence**: every showcase recipe that goes through `BuildFinalizeOutput(plan)` ships the same hardcoded env README prose regardless of what the recipe's `plan.Targets` actually configures. The prose makes claims the plan cannot back up.

**Why this slipped past the writer sub-agent**: Cx-1 WRITER-SCOPE-REDUCTION (commit `c9da867`, v8.112.0) explicitly removed env READMEs from the writer's scope — for good reason (to shrink the writer's paraphrase surface after v37's F-9 ghost env dirs). But Cx-1 left env READMEs as hardcoded Go strings instead of moving them onto a surface that the spec-content-surfaces.md tests could reach. The writer dispatch prompt (60KB, correctly stitched by Cx-5 — see verdict §3) teaches the per-surface tests + classification + counter-examples. That teaching reaches the writer. It does not reach `recipe_templates.go`. The Go source has never been run through the surface tests.

---

## 2. What editorial-review actually reported (the first pass misread this)

**What the first pass wrote**: retry-cycle-18 timestamp 2026-04-22T11:00:36Z attribution "editorial-review returned clean".

**What editorial-review actually returned** (per [`SESSIONS_LOGS/subagents/agent-afa52e6ec9350b1b3.jsonl`](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/SESSIONS_LOGS/subagents/agent-afa52e6ec9350b1b3.jsonl) final assistant turn): **5 CRIT + 3 WRONG + 2 STYLE**. Post-reviewer-inline-fix: **5 CRIT (unchanged — caller revision required)** + 2 WRONG + 2 STYLE.

Five of the six CRITs were direct hits on content produced by `recipe_templates.go`:

| # | Surface | CRIT class | Root source |
|---|---|---|---|
| 1 | `1 — Remote (CDE)/README.md` L24 | **Fabricated mechanism** — claims dev container image differs between tier 0 and tier 1. Both tiers' import.yaml declare identical `nodejs@24` with identical `zeropsSetup: dev`; no evidence of a distinct image. | `recipe_templates.go:envDiffFromPrevious(1)` L273-276 hardcodes "Runtime containers carry an expanded toolchain — IDE Remote server, shell customizations, language-specific debug tools" and "only the dev container image differs". Neither claim is plan-derived. |
| 2 | `4 — Small Production/README.md` L33 vs `4/import.yaml:60-62` db comment | **Cross-surface divergence** — README says `mode` is immutable and HA happens in a new project; import.yaml's db comment (at review time) said "Promotes from NON_HA with a mode change" on this tier's services. | Template-level: `envPromotionPath(4)` at L345 vs `GenerateEnvImportYAML` env-comment block for env 4 db. Two hardcoded prose generators producing contradictory claims about the same behavior. |
| 3 | `3 — Stage/README.md` L42 | **Factually wrong** — "Stage hits the same DB as dev on tiers 0-2". Each tier declares its own `project.name` (e.g. `nestjs-showcase-agent`, `nestjs-showcase-remote`, `nestjs-showcase-local`, `nestjs-showcase-stage`). | `recipe_templates.go:envOperationalConcerns(3)` L382 hardcodes this verbatim. The claim has been wrong in every showcase run that used this function since v8.94. |
| 4 | `appdev/README.md` L166 (before fix) | **Folk-doctrine** — writer-authored: "The benign `zcli` warning 'dist/~ paths not found' shows up in build logs regardless of success". Invented reassurance not grounded in the `deploy-files` platform guide. | Writer sub-agent (not Go source). This is the ONE CRIT the writer is responsible for. |
| 5 | `4 — Small Production/README.md` L20-27 | **Cross-surface divergence** — tier 4 "What changes vs stage" omits that NATS moves to `mode: HA` at tier 5, which tier 5 import.yaml L94-104 declares. | `recipe_templates.go:envDiffFromPrevious(4)` L292-297 and `envDiffFromPrevious(5)` L298-306 — the bullet listing what's different at tier 5 is missing from the tier-4-forward narrative. Hardcoded prose drift between two hardcoded functions. |
| 6 | `apidev/README.md` L316 gotcha | **Reclassification reroute** — writer classified "dev setup needs own initCommands" as framework-invariant; reviewer says it's a scaffold-decision that belongs in `CLAUDE.md`, not in the gotcha fragment. | Writer sub-agent. One wrong-surface placement per the spec's classification taxonomy. |

**The split: 4 of 6 CRITs are in Go-source prose; 2 of 6 are in writer output.** The Go source hasn't been run through the spec tests, so it ships with fabricated/wrong content on every recipe. The writer has been through the spec tests (via its brief) and produced 2 CRITs that its brief was supposed to prevent.

**What the main agent did**: took editorial-review's feedback seriously. Between 10:53:14Z and 10:59:27Z (six minutes), the main agent:

- Edited `1 — Remote (CDE)/README.md` (CRIT 1) — softened the "expanded toolchain" claim but retained its core framing
- Edited `3 — Stage/README.md` (CRIT 3) — replaced "Stage hits the same DB as dev" with "Stage runs its own project with its own DB"
- Edited `4 — Small Production/README.md` + its `import.yaml` (CRIT 2, 5) — aligned mode-immutability claim, added NATS HA-at-tier-5 bullet
- Edited `appdev/README.md` at both mount paths (CRIT 4) — removed the folk-doctrine paragraph
- Edited `apidev/README.md` + `apidev/CLAUDE.md` (CRIT 6) — moved the `initCommands` teaching from gotcha to CLAUDE.md
- Edited `ZCP_CONTENT_MANIFEST.json` at both paths to reflect the reroute
- Re-attempted close/editorial-review 4 times before it attested at 11:00:36Z

The deliverable tree now has the corrected content. The Go source still has the original bugs. `git grep 'Runtime containers carry an expanded toolchain' internal/workflow/recipe_templates.go` = one hit. `git grep 'Stage hits the same DB as dev' internal/workflow/recipe_templates.go` = one hit. The NEXT recipe run that calls `BuildFinalizeOutput(plan)` will regenerate the same CRITs.

---

## 3. Why the first pass missed this

Same meta-failure shape as v36 CORRECTIONS.md §2.7: **"Artifact-shape as proxy for content verification."** I read the six writer-authored files + one env README + the start of one env import.yaml, found them substantive, and graded them `pass`. I did not:

- Cross-reference prose claims against the plan data the spec says they should be grounded in
- Check git-grep for hardcoded prose under `internal/workflow/` that could fabricate claims off plan context
- Read the editorial-review sub-agent's full completion payload before summarizing retry-cycle-18 as "returned clean"
- Apply the spec's single-question tests myself (e.g. "does L273's claim about 'expanded toolchain' survive a diff against env 1's import.yaml?") — the answer would have immediately revealed CRIT 1

The harness caught zero of this because the harness operates on filesystem shape + retry-cycle counts, not content-vs-source grounding.

**The user's framing — "you want to add stripping instead of addressing why the wrong content got there"** is correct about my original Cx-5b / Cx-4b proposals:

- **Cx-5b (dispatch guard auto-enforcement)** fixes the paraphrase surface but does not fix hardcoded Go prose — the guard would have let this run ship unchanged because the Go source compiles to a deterministic brief.
- **Cx-4b (export whitelist extension)** gets the manifest into the tarball but does not fix any prose.
- **Both proposals treat symptoms in the dispatch/export layer.** Neither addresses the Go source that never ran through the spec.

The "stripping" pattern the user named: the main agent strips fabricated text from the rendered deliverable so editorial-review attests, but the Go source retains the text and the next run regenerates it. That's exactly what happened at retry-cycle-18: 5 CRITs were edited in the deliverable; 5 CRITs still ship in `recipe_templates.go` at HEAD.

---

## 4. Corrected defect closure matrix (superseding verdict §2)

Only the rows that changed are listed; the rest of verdict §2 stands.

| Target | Corrected status | Evidence |
|---|---|---|
| Writer content quality (verdict §3 bullet 6) | **PARTIAL — 1 of 2 per-codebase CRITs caught by editorial-review required caller revision** | [CORRECTIONS.md §2] CRIT 4 (folk-doctrine "benign zcli warning") and CRIT 6 (scaffold-decision routed as framework-invariant). Writer brief teaching reached the writer and still yielded 2 spec-violating items. Not a writer-sound pass. |
| Env README quality (verdict §3 bullet 8) | **FAIL — 3 hardcoded Go-source CRITs (1, 2, 3, 5) present at HEAD** | [CORRECTIONS.md §2] Every showcase recipe regenerated via `BuildFinalizeOutput` ships these. Main agent edited the deliverable; source code at `recipe_templates.go:273, 345, 382` and the envDiffFromPrevious chain still has them. |
| F-NEW — Go-source content authorship bypasses spec-content-surfaces.md | **NEW — OPEN** | `recipe_templates.go:envAudience/envDiffFromPrevious/envPromotionPath/envOperationalConcerns` never run through the per-surface single-question tests at [docs/spec-content-surfaces.md:298-310](../../spec-content-surfaces.md#L298). Every hardcoded switch case is a place a fabricated claim can ship undetected. |
| F-17-runtime (paraphrase) — severity | **UNCHANGED — still OPEN**, but the in-run impact was more limited than verdict §1 implied | Editorial-review received a 72%-truncated brief and still caught 5 CRITs of the 6 spec-violations present. The sub-agent's training + its partial brief covered the tests. The paraphrase is a real architectural issue but did not functionally blind editorial-review. |

---

## 5. Corrected v39 fix stack (superseding verdict §7)

Ordered by root-cause distance. The first entry is the highest-leverage fix the user's critique pointed at.

1. **Cx-GO-TEMPLATE-GROUND** (new — headline v39 fix). Two sub-commits:
   - **a. Reshape `recipe_templates.go`** so every prose-generating function takes `plan *RecipePlan` and computes claims from plan data instead of hardcoding them. E.g., `envDiffFromPrevious(envIndex, plan)` computes the diff between `plan.EnvTemplates[envIndex-1]` and `plan.EnvTemplates[envIndex]` and emits only bullets that map to a real field difference. If no difference exists on a dimension, the bullet is omitted. Fabrication becomes architecturally impossible because the function cannot describe a difference that isn't in the plan.
   - **b. Add `TestFinalizeOutput_PassesSurfaceContractTests`** — a gold-test under `internal/workflow/` that calls `BuildFinalizeOutput(fixturePlan)` and runs the spec's Surface 2 single-question test programmatically against each emitted env README: for every claim paragraph, does the claim correspond to a real field difference in the fixture's import.yaml? Any hardcoded claim that cannot be ground-truthed against the rendered yaml fails. This becomes the CI wall that prevents the v38 class of CRIT from recurring.
   - Alternative formulation if (a) is too invasive: move env-README authoring to the writer sub-agent (reverse Cx-1's env README exclusion) AND add a dispatch-guard-enforceable manifest-class for env READMEs. Lower-quality fix because it re-expands the writer surface Cx-1 shrunk — so (a) is the preferred path.

2. **Cx-5b — DISPATCH-GUARD-AUTO-ENFORCE** (unchanged from verdict §7). Still needed: the editorial-review 72% paraphrase is architecturally wrong even if its functional impact was contained in v38.

3. **Cx-4b — EXPORT-ROOT-FILE-EXTEND** (unchanged from verdict §7). Still needed: the manifest is the writer's honesty declaration and must reach the deliverable.

4. **Cx-WRITER-SPEC-TEACHING-TIGHTEN** (new — addresses the 2 writer CRITs from v38). The writer brief reaches the writer correctly (the engine stitches it byte-identically; only editorial-review was paraphrased). But the writer still produced CRIT 4 (folk-doctrine reassurance) and CRIT 6 (wrong-surface placement). Either the teaching needs to be sharper (more counter-examples showing these exact failure patterns) OR the writer needs a self-review pass that runs each gotcha/IG item through the single-question test before declaring completion. Ship as an atom tightening + an added step in the writer completion-shape.

5. **Cx-3b — ENV-COMMENT-PRINCIPLE-ATOM-STRENGTHEN** (unchanged from verdict §7). F-21 atom prevention is still partial.

6. **Cx-8b — HARNESS CASE-INSENSITIVITY** (unchanged from verdict §7).

The first item is the headline. The rest are backlog.

---

## 6. Lesson to institutionalize (amending verdict §10)

Verdict §10 said: "Architectural fixes must ship with runtime enforcement." That's true and stands.

This document adds a parallel rule: **"Content surfaces emitted by engine code must run through the same spec tests as content surfaces emitted by sub-agents."**

Cx-1 was right to shrink the writer's scope (less paraphrase surface). Cx-1 was wrong to leave the removed content as hardcoded Go strings without a compensating quality gate. Any time a future Cx moves content authorship from a sub-agent (which has a spec-teaching brief) to engine code (which does not), the move MUST ship with an automated test that re-applies the spec's single-question tests to the engine-emitted output. Otherwise the brief's teaching stops at the code boundary and fabrications ship unopposed.

The check list for moving content from sub-agent to engine:

- [ ] Does the engine function take the `plan` as a parameter?
- [ ] Do every claim in the engine's output correspond to a plan field, a platform-YAML field, or a citable guide?
- [ ] Is there a gold test that runs the spec's per-surface question against the output for a fixture plan?
- [ ] If a future plan shape would make a hardcoded claim wrong (e.g. a recipe without a DB, a recipe with a different env tier layout), does the function degrade gracefully?

If any answer is "no", the move is not complete.

---

## 7. What stays from the verdict

Unamended:
- **Verdict direction: PAUSE** — confirmed; the v38 run's shipped deliverable has been manually stripped to pass editorial-review, but the Go source at HEAD still has the bugs + F-17-runtime is still open + F-23 still open.
- **Defect closures**: F-9, F-12, F-13, F-24 closed per harness bars [machine-report.structural_integrity.B-15 observed=0] + [B-17 observed=0] + [B-18 observed=0] + [checklist retry-cycle-20].
- **Cx-5 is architecturally correct at HEAD**: `BuildSubagentBrief` + `VerifySubagentDispatch` are sound functions. Only their auto-enforcement is missing.
- **Writer dispatch prompt is byte-sound** (modulo a 4-byte `\u2014`→em-dash encoding artifact).
- **Cx-1's scope reduction premise (shrink paraphrase surface) is sound**. Its execution (leave the removed content as hardcoded Go) is incomplete.

---

## 8. Evidence index (reproducible from this directory)

- Editorial-review sub-agent final payload: [`../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/SESSIONS_LOGS/subagents/agent-afa52e6ec9350b1b3.jsonl`](../../../Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v38/SESSIONS_LOGS/subagents/agent-afa52e6ec9350b1b3.jsonl) — last assistant turn carries the completion payload with the 5 CRIT table.
- Main-agent edit timeline between editorial-review return and close-step attest: main-session.jsonl lines 771-852, timestamps 10:53:14Z → 11:00:36Z.
- Go source at HEAD with unfixed fabrications: [`internal/workflow/recipe_templates.go:273, 345, 382`](../../../internal/workflow/recipe_templates.go). `git grep -n 'expanded toolchain\|Stage hits the same DB' internal/workflow/recipe_templates.go` reproduces both.
- Spec-content-surfaces: [`docs/spec-content-surfaces.md`](../../spec-content-surfaces.md) §5 "Per-surface test cheatsheet" L298-310 — the single-question tests that need to be applied to engine-emitted output.
