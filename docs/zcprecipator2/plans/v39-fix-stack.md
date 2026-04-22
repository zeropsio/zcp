# plans/v39-fix-stack.md — context-tightening first, thin safety net behind

**Status**: TRANSIENT per CLAUDE.md §Source of Truth #4. Archive to `plans/archive/` after v39 verdict ships.
**Prerequisites**: [`../runs/v38/verdict.md`](../runs/v38/verdict.md), [`../runs/v38/CORRECTIONS.md`](../runs/v38/CORRECTIONS.md), [`../../spec-content-surfaces.md`](../../spec-content-surfaces.md).
**Target tag**: `v8.113.0`.
**Estimated effort**: 4–5 days focused work.

This plan supersedes an earlier 11-commit enforcement-heavy version (retained in git history at `ff3dcfb`). The direction shift was made on 2026-04-22 after user feedback: the earlier plan treated "catch bad output with machine checks" as the primary fix. v38 evidence argues the opposite — the writer had a 60KB brief and still produced folk-doctrine, which says teaching volume is the problem, not missing enforcement. The right fix is tighter context given at authoring time, not a gauntlet of post-hoc checks.

---

## 0. What the v38 data actually said

Four content-emitting entities. Their output quality maps INVERSELY to context size:

| Entity | Brief / teaching size | Output quality in v38 |
|---|---|---|
| Writer sub-agent | 60KB brief | 2 CRITs (folk-doctrine + wrong-surface) |
| Editorial-review sub-agent | 13KB dispatched (72% paraphrased from 47KB engine brief) | Caught 5 CRITs in writer + engine output |
| Main agent (envComments, zerops.yaml comments) | ~1 sub-section of workflow guide | F-21 invented "2 GB quota"; no voice-class fires in v38 |
| Engine `recipe_templates.go` (env READMEs) | Zero spec teaching; hardcoded Go strings | 4 of 6 editorial-review CRITs came from this source |

The editorial-review sub-agent received 72% LESS teaching than the writer and caught MORE defects than the writer prevented. That's the clearest signal in the data that brief-volume isn't the solution — **tight, targeted context at the right moment beats comprehensive teaching up front**.

---

## 1. The three design principles

**(a) Agents see source-of-truth, not inventions.** When the agent authors content that claims a fact, the fact is visible to the agent at the moment of authoring. Examples: when writing envComments, the rendered yaml for that env is in the agent's context. When writing gotchas, the facts log is the input (not the agent's memory). When writing CLAUDE.md, the scaffold's actual file tree is in context.

**(b) Examples beat rules.** Replace prose teaching with annotated examples. 3-5 examples per surface (two good, two bad tagged with specific failure) is more effective per byte than 2KB of prose rules. Pattern-matching against concrete examples fits how agents work; prose rules get paraphrased and lose precision.

**(c) Right-size the brief to the role.** Writer drops from 60KB to ~18KB — classification tables move to runtime lookups; wrong-role atoms (fact-recording-discipline) get dropped. Main agent gets a ~3KB comment-authoring topic injected at generate/finalize entry, not a one-paragraph mention inside a 3000-line workflow doc.

---

## 2. The 5-commit stack

### Commit 1 — Engine-template grounding

**What**: parametrize the four env-README prose functions in [`internal/workflow/recipe_templates.go`](../../../internal/workflow/recipe_templates.go) so every claim is computed from plan data. Add a gold test that enforces three-way equality: for every emitted bullet, (struct field value) == (yaml-generator's emitted text for that field) == (prose bullet's claim about that field). Fabrication becomes structurally impossible.

**Why this stays in the plan**: there's no agent authoring these — it's Go code emitting strings. Context-tightening doesn't apply; the Go source itself needs the fix.

**Why headline**: 4 of v38's 6 editorial-review CRITs came from this one file. Fixing it removes the single largest source of content defect.

**Day breakdown** (2–3 days):

- **Day 1 — bullet audit.** Inventory every hardcoded bullet in `envAudience / envDiffFromPrevious / envPromotionPath / envOperationalConcerns` (~40-50 bullets). Classify each:
  - (a) plan-backed: already corresponds to a field in `writeSingleService` yaml generator (mode, minContainers, zeropsSetup, priority).
  - (b) prose-only: bullet claims something no yaml field backs ("Backups become meaningful at this tier"). Risk class.
  - (c) system-invariant: true of every recipe by construction ("Each tier's import.yaml declares a distinct project.name").
- **Day 2 — refactor.** Extract `EnvTemplate` struct. Rewrite prose functions to compose from struct fields (for a) + system-invariant template list (for c). For (b) bullets: user decides per-bullet — promote to schema + add yaml-generator support, or drop the bullet. Skipping this audit causes either silent teaching loss or fabrication migration into schema.
- **Day 3 — gold test.** `TestFinalizeOutput_PassesSurfaceContractTests` — for each generated bullet, assert three-way equality. Any divergence fails with pointer at which layer broke.

**Files touched**: `internal/workflow/recipe.go` (add EnvTemplate), `recipe_templates.go` (rewrite 4 prose functions), `recipe_templates_test.go` (gold test), possibly `recipe_templates_import.go` (if (b) bullets promote to schema + yaml support).

**Acceptance on v39**: editorial-review reports 0 CRITs on any env README.

---

### Commit 2 — Manifest export whitelist

**What**: extend the root-file whitelist at [`internal/sync/export.go:236`](../../../internal/sync/export.go#L236) to include `ZCP_CONTENT_MANIFEST.json`.

**Why**: Cx-4 MANIFEST-OVERLAY in v8.112.0 stages the manifest into the recipe output dir. The export function only whitelists `TIMELINE.md` + `README.md` as root files, so the manifest gets dropped before the tarball is written.

**Files touched**: `internal/sync/export.go`, `internal/sync/export_test.go`.

**RED test**: `TestExportRecipe_IncludesRootManifest`.

**Acceptance**: `find nestjs-showcase-v39/ -name "ZCP_CONTENT_MANIFEST.json"` returns a file.

**Estimated**: 30 minutes.

---

### Commit 3 — Content-authoring context pack per step

**What**: new engine behavior at each content-emission substep. The engine injects role-appropriate, moment-appropriate context into the agent's guidance. Three concrete cases:

**3a. Generate step (main agent writes zerops.yaml):**
When the agent enters the generate step, guidance includes (beyond the current step-entry teaching):
- 3-5 annotated example zerops.yaml comment blocks from a curated example bank. Format: each example shows a comment plus its verdict (PASS — principle-level, trade-off explained / FAIL — field narration / FAIL — journal voice) with one sentence of rationale.
- A pointer: "for any field whose behavior you're commenting about, call `zerops_knowledge topic=X` and paraphrase the guide's framing. Don't invent mechanism explanations."

**3b. Finalize step (main agent writes envComments):**
When the agent starts `generate-finalize`, the engine FIRST renders the import.yaml for the current env with schema only (no comments), and shows it to the agent as input:
```
Here is env 4's import.yaml as it will render. Your envComments for this env describe WHY each service block looks the way it does.

<yaml content>

Write comments grounded in the yaml above. Do not claim a number that isn't in the yaml. If a tier decision isn't visible in the yaml, don't comment about it.
```
Plus 3-5 example envComment blocks with verdicts.

**3c. Deploy/readmes (writer sub-agent dispatch):**
The writer's engine-stitched brief now includes, as a pre-loaded input block:
- Per-codebase: abbreviated file tree + the already-rendered zerops.yaml of that codebase (so gotchas can reference real patterns, not invented ones).
- Facts-log subset filtered to facts routed to `content_gotcha` / `content_ig` (via Commit 4 below).
- 2-3 annotated examples per surface (intro, IG item, gotcha, CLAUDE.md section). Drawn from a curated examples bank.

**The example bank (new)**: `internal/content/examples/` contains `.md` files with frontmatter declaring:
```
---
surface: gotcha | ig-item | intro | claude-section | env-comment | zerops-yaml-comment
verdict: pass | fail
reason: one-sentence tag (folk-doctrine | journal-voice | self-referential | platform-invariant-ok)
---
<example body>
```
Seeding: ~15-20 files extracted from `spec-content-surfaces.md §11 counter-examples` (failure cases) plus hand-picked winners from v38 post-correction content (pass cases). Each subsequent run's editorial-review findings can be promoted to the bank (new bad examples) or the post-fix versions (new good examples).

**Engine mechanism**: a new helper `examples.SampleFor(surface, n)` returns n rotating examples (mix good + bad). Called by substep guidance composers at generate/finalize/deploy-readmes entry.

**Why this is the headline context-tightening commit**: replaces the need for ~5 machine checks. If the writer sees 3 good gotcha examples alongside the citation-map requirement, it pattern-matches against known-good shape. Folk-doctrine emerges when the agent is working from memory of rules; examples close the gap.

**Files touched**:
- `internal/content/examples/` — new directory, ~15-20 seed files.
- `internal/workflow/examples.go` — new `SampleFor` helper + example-bank loader.
- `internal/workflow/recipe_guidance.go` — extend step-entry composers to inject examples at the right moments.
- `internal/workflow/subagent_brief.go` — `buildWriterBriefRendered` appends the pre-loaded input block (yaml + facts-log + examples) before the brief is handed to the writer.
- `internal/content/topics/comment-style.md` — new topic for main-agent comment guidance, fetchable via `zerops_knowledge`.

**RED tests**:
- `TestGenerateStepGuidance_IncludesExamples` — asserts step-entry guidance contains ≥3 example blocks.
- `TestFinalizeStepGuidance_IncludesRenderedYaml` — asserts envComments guidance shows the rendered yaml for the current env.
- `TestWriterBrief_IncludesFactsLogAndExamples` — asserts writer brief input block includes routed facts + examples.
- `TestExamplesBank_FrontmatterValid` — schema check on the bank.

**Acceptance on v39**:
- envComments contain no claimed numbers absent from the rendered yaml (F-21 class).
- zerops.yaml comments contain no journal voice.
- Writer-produced gotchas have citation map coverage = 100% of citation-map-topic bullets.

**Estimated**: 2 days.

---

### Commit 4 — Knowledge-lookup as workflow step

**What**: writer's completion shape (per [`internal/content/workflows/recipe/briefs/writer/completion-shape.md`](../../../internal/content/workflows/recipe/briefs/writer/completion-shape.md)) requires a new `citations` array. For every gotcha or IG item whose topic appears in the citation map, the corresponding citation entry must carry a `guide_fetched_at` timestamp proving `zerops_knowledge` was called on that topic during writing. Missing timestamp = engine refuses `action=complete substep=readmes` with a specific remediation.

**Why one hard gate survives**: this turns a judgment call ("is this bullet folk-doctrine?") into a file-existence check ("did the knowledge fetch happen before the bullet was written?"). Zero subjectivity; trivially machine-verifiable. And it forecloses the folk-doctrine class at its root: if the agent looked up the guide, it's paraphrasing an authoritative source; if it didn't, the bullet can't ship.

**Additionally**: `zerops_record_fact` gets a nudge (not a refusal) when `routeTo` is missing — engine response includes a "based on type=X, likely route is Y; confirm or pass `routeTo` explicitly." Records land with routing most of the time; writer's context pack (Commit 3c) can filter facts by routing.

**Files touched**:
- `internal/content/workflows/recipe/briefs/writer/completion-shape.md` — add `citations` field spec.
- `internal/workflow/recipe_step_checks.go` — add `readmes_citations_present` check at `complete substep=readmes`.
- `internal/workflow/fact_record.go` — nudge response on missing routeTo.
- `internal/workflow/fact_record_test.go` — test the nudge.

**RED tests**:
- `TestCompleteReadmes_RequiresCitationTimestamps` — fixture manifest with citation-map-topic gotcha missing `guide_fetched_at` fails completion.
- `TestRecordFact_NudgeOnMissingRouteTo`.

**Acceptance on v39**: writer's completion payload includes `citations` array; zero `readmes_citations_present` failures; every citation-map-topic bullet paraphrases its guide.

**Estimated**: 4 hours.

---

### Commit 5 — Writer brief slim + canonical task list at session start

**Two small combined changes:**

**5a. Writer brief slim (60KB → ~18KB):**
- Remove `principles.fact-recording-discipline` from `writerPrinciples()` — wrong role (writer reads facts; doesn't record).
- Replace `briefs.writer.classification-taxonomy` + `briefs.writer.routing-matrix` (11KB combined) with one paragraph pointing at new `zerops_workflow action=classify` runtime lookup for per-item override cases. The runtime action reads the same Go-side routing matrix.
- Trim `content-surface-contracts.md` — keep the single-question tests for each surface; drop the "does NOT belong here" negative-form prose (replaced by bad-example injection via Commit 3c).
- Keep everything else.

**5b. Canonical task list published at session start:**
When main agent calls `zerops_workflow action=start workflow=recipe tier=<tier>`, the engine response includes a `startingTodos` array with the canonical 19-substep breakdown. Main agent pastes it into its first TodoWrite call; marks items done as they close. No re-planning, no re-compression mid-session.

**Why together**: both are "stop asking the agent to reconstruct what the engine already knows." Writer doesn't need 11KB of classification rules in its brief — the engine has them; writer asks when it needs to. Main agent doesn't need to derive a substep breakdown — the engine has it; ship it at start.

**Files touched**:
- `internal/workflow/atom_stitcher.go` — `writerBriefBodyAtomIDs()` + `writerPrinciples()` edits.
- `internal/workflow/classification.go` (new, small) — Go-side routing matrix extracted from the two atoms; consumed by new `action=classify` handler.
- `internal/tools/workflow.go` — add `action=classify` handler.
- `internal/workflow/recipe_substeps.go` — `startingTodos` helper.
- `internal/tools/workflow.go` — include `startingTodos` in `action=start` response.
- `internal/workflow/subagent_brief_test.go` — assert brief size ≤ 25KB.

**RED tests**:
- `TestBuildWriterBrief_UnderSizeLimit` — 25KB.
- `TestClassifyAction_ReturnsRouteTo` — 6 cases (one per class).
- `TestWorkflowStart_IncludesStartingTodos` — asserts `startingTodos` array populated.

**Acceptance on v39**:
- Writer brief size ≤ 25KB.
- Main-session shows 1–3 TodoWrite calls instead of v38's 28 (starter list + mark-done updates).

**Estimated**: 4 hours.

---

## 3. What explicitly DROPS from the earlier plan

The earlier 11-commit plan (git SHA `ff3dcfb`) included several commits now dropped:

- **Dispatch-guard auto-enforcement** — dropped. F-17's cause was a 60KB brief the main agent compressed. A 18KB brief with no redundancy is nothing to paraphrase. The class self-extinguishes.
- **Writer self-review-as-step-gate** (folk-doctrine regex, wrong-surface detector, voice regex) — dropped. Commit 4 closes folk-doctrine via forced knowledge-lookup at authoring time. Commit 3 (examples + yaml visibility) closes wrong-surface at authoring time. Voice regex checks become redundant with example-driven teaching.
- **Env-comment-scaffold-decision-placement check** — dropped. Commit 3b shows the agent the rendered yaml before authoring envComments; scaffold-decision-in-wrong-place emerges from agents working from memory, not source-of-truth. Closed at source.
- **Stripping-visibility-warn** — dropped as standalone. Commit 1 removes the Go-source fabrications so there's nothing to strip. If new engine-side content emitters appear in future work, the invariant added to PLAN.md §1.5 (gold tests for all engine-emitted content) catches them at compile-time.
- **Surface-doc-comment-lint** — deferred. PLAN.md §1.5 makes it a future invariant; lint enforcement lands when a second engine-side emitter appears.

Safety net kept thin:
- `readmes_manifest_exists_and_valid_json` — mechanical, not judgment.
- `readmes_citations_present` — the one hard gate surviving (Commit 4).
- Existing `fragment_*` / `comment_ratio` / factual_claims checks — existing, unchanged.
- Editorial-review stays as final adversarial pass. Target drop: 5 CRITs (v38) → ≤ 1 CRIT (v39).

---

## 4. Retrospective harness run (before v8.113.0 tag)

Before tagging, run `zcp analyze recipe-run` against v38 deliverable with the v39 checks active. Expected:

- **Commit 1 gold-test fails on v38** (catches CRIT #1 "expanded toolchain" retroactively — the hardcoded claim has no backing field).
- **Commit 4 `readmes_citations_present` fails on v38** (writer's manifest has no `guide_fetched_at` field at all in v38 format).
- **Writer brief size** — if we invoke `BuildSubagentBrief` against HEAD with fixture plan and check `len(Prompt) ≤ 25KB`, passes. On v38 (pre-slim), fails.
- **Classify action** — against v38's facts log, routing-matrix coverage 100% of fact types.

---

## 5. v39 commission spec

```
TIER:                 showcase
SLUG:                 nestjs-showcase
FRAMEWORK:            nestjs
TAG:                  v8.113.0
MUST_REACH:           close-step complete + ZCP_CONTENT_MANIFEST.json in deliverable
MUST_PASS:            writer brief size ≤ 25KB at dispatch
MUST_PASS:            every writer gotcha/IG item with citation-map topic has
                      guide_fetched_at timestamp
MUST_PASS:            zero editorial-review CRITs on env READMEs (Commit 1 grounds them)
MUST_PASS:            zero envComment claimed numbers absent from rendered yaml
MUST_CONVERGE:        readmes retry rounds ≤ 1, finalize rounds ≤ 1, editorial-review
                      attest attempts ≤ 2
```

**During-run tripwires**:

- TodoWrite call count in main-session (v38 had 28) should drop to ≤ 5 (starter + periodic updates).
- Writer calls `zerops_knowledge` at least once per citation-map-topic bullet it writes.
- `generate-finalize` response includes the rendered yaml as an input context block for each env.

---

## 6. What v39 is NOT doing

- C-15 recipe.md deletion (R2-R7 per [`../PLAN.md`](../PLAN.md) §2). Deferred until v39 PROCEED.
- Framework diversity (stays nestjs).
- Minimal-tier independent track.
- Publish-pipeline.

---

## 7. Parallelization

| Commit | Depends on | Parallel-safe with |
|---|---|---|
| 1 Engine-template grounding | — | 2, 4, 5 |
| 2 Manifest export whitelist | — | any |
| 3 Content-authoring context pack | — (example bank is new infrastructure) | 1, 2, 4 |
| 4 Knowledge-lookup workflow step | — | 1, 2, 3, 5 |
| 5 Writer brief slim + starter todos | — | 1, 2, 3, 4 |

Mostly parallel. Single-person drive: 4–5 days sequential. Two-person parallel: 2–3 days.

---

## 8. Exit criteria

- [ ] 5 commits merged to main with RED→GREEN cycle each.
- [ ] `go test ./... -race -count=1` green.
- [ ] `make lint-local` green.
- [ ] Retrospective run against v38 shows the new checks catch known v38 defects per §4.
- [ ] v8.113.0 tagged + pushed.
- [ ] Slot block in [`../HANDOFF-to-I10-v39-prep.md`](../HANDOFF-to-I10-v39-prep.md) filled with commit SHAs.
- [ ] User commissioned v39.
- [ ] `runs/v39/{machine-report.json, verification-checklist.md, verdict.md}` present; verify-verdict hook passes.
- [ ] Verdict decision shipped.

If v39 clears, C-15 (recipe.md deletion) becomes the next handoff target per [`../PLAN.md`](../PLAN.md) §3. If PAUSE, next handoff targets whichever layer v39 exposed — likely a novel content-quality class requiring a new example-bank seed, not a new check.
