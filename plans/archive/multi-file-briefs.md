# Multi-file briefs — closure of run-30/31 Fix #1 token-cap regression

Author: 2026-05-07. Closes the cap-treadmill across runs 22-31 (44→48→52→56→60→64→72→76→66 KB) by changing the brief transport shape from "one big file the agent reads in one shot" to "an index plus N parts the agent reads in a known order." Each part is bounded by construction; the body as a whole is unbounded.

Background context lives at `docs/zcprecipator3/runs/31/ANALYSIS.md` §"F-track verdict update" + §"Sim-driven refinement plan" and `docs/zcprecipator3/runs/30/ANALYSIS.md` §"Fix #1".

## The problem in one paragraph

Claude Code's Read tool has a 25K real-token hard cap. The composer-side `ReadToolTokenCeiling=33000` (estimated under `len/2`) held in unit tests against a synthetic plan with `nil` facts. With realistic fact corpora (run-31 nestjs-showcase: 142 facts, ~93 in cc-api scope), real briefs land at 78-94 KB / 36-39K real tokens. Three sub-agents tripped the ceiling in every recent run; agents recover with offset+limit chunking that invalidates prompt cache and adds 20-60s per dispatch.

Content refinement (`cross-service-urls.md` → `*-summary.md`, worker-supplement embedding → one-line pointer) saves ~7-10 KB but never reaches the 62 KB byte target needed to fit under 25K real tokens. The teaching that's load-bearing IS load-bearing — it can't be removed without giving up content quality.

## Architectural answer

**Multi-file briefs.** The composer emits an `index.md` plus N part files under `<outputRoot>/.briefs/<kind>-<codebase>-<unixnano>/`. The dispatch envelope returns `briefPath` pointing at the index; the index contains a "Read these in order" instruction listing the parts by absolute path. Sub-agents discover parts via the index — there's no `parts: []string` field on the dispatch envelope that would have to round-trip through MCP.

### Why index-as-entry-point (not envelope-carries-parts)

- One pointer through the MCP boundary = one source of truth. Agents are taught "read briefPath first thing"; that contract doesn't change.
- The deterministic split + ordering live in the file the agent actually reads. If we hand the agent a list via the envelope, two competing schemas exist (envelope's vs index's) and they can drift.
- Index can carry per-part one-line descriptions so the agent picks the right part without round-tripping through Read on every part.

### Per-part token cap

Use `PerPartTokenCeiling = 22000` estimated tokens (≈44 KB bytes under `len/2` estimator). This is below the 33000 real-Read-tool ceiling with margin for the index pointer + JSON envelope inflation. The byte equivalent (`PartFileCap = 44 * 1024`) is exposed for parts whose composer wants byte arithmetic.

Pick token-side authority because the whole architectural answer is "fit under the Read-tool's token cap"; the byte-side cap is a derived quantity. Both constants live in `briefs.go`; the part-writer abstraction enforces the token-side cap.

### Index.md shape

```markdown
# Engine brief — codebase-content (api / nestjs-showcase)

The brief composes from N parts. Read each part in this order before authoring; each part is sized to fit under the Read-tool single-shot token cap.

## Read order

1. `<outputRoot>/.briefs/codebase-content-api-<ts>/part-1-phase-entry.md` — phase entry + synthesis workflow + citation guides
2. `<outputRoot>/.briefs/codebase-content-api-<ts>/part-2-platform.md` — platform principles
3. `<outputRoot>/.briefs/codebase-content-api-<ts>/part-3-cross-service.md` — NATS shapes + cross-service URLs
4. `<outputRoot>/.briefs/codebase-content-api-<ts>/part-4-yaml-style.md` — Zerops-knowledge attestation + yaml comment style
5. `<outputRoot>/.briefs/codebase-content-api-<ts>/part-5-context.md` — codebase metadata + facts + parent recipe baseline + sibling sub-agent note

## Recipe-level context

(slug, codebase, etc — same as the inline-prompt header today)

## Closing notes from the engine

(close-phase invocation — same as today)
```

The recipe-level context + closing notes ride on the index because they're cheap (under 5 KB) and the agent needs them at every authoring step.

### Part-file naming convention — deterministic ordering

`part-<N>-<kebab-slug>.md` where N is a 1-indexed integer (zero-padded if N > 9 ever needed; 5-7 parts is the steady state). Slug is taken from a fixed table per kind so two runs of the same composer produce byte-identical names:

**codebase-content** parts:
1. `part-1-phase-entry.md` — phase_entry/codebase-content.md + synthesis_workflow.md + citation guides + (worker KB pointer if applicable)
2. `part-2-platform.md` — platform_principles.md
3. `part-3-cross-service.md` — nats-shapes.md + cross-service-urls-summary.md (conditional per `shouldLoadNATSShapes` / `shouldLoadCrossServiceURLs`)
4. `part-4-yaml-style.md` — zerops-knowledge-attestation.md + yaml-comment-style.md
5. `part-5-context.md` — codebase metadata + filtered facts + cross-codebase facts + pointer block + embedded parent baseline + sibling sub-agent note

**env-content** parts:
1. `part-1-phase-entry.md` — phase_entry/env-content.md + per_tier_authoring.md
2. `part-2-cross-service.md` — cross-service-urls.md + nats-shapes.md (conditional)
3. `part-3-yaml-style.md` — zerops-knowledge-attestation.md + yaml-comment-style.md
4. `part-4-context.md` — capability matrix + cross-tier deltas + emitted facts + contracts + plan snapshot + parent recipe pointer

**refinement** parts:
1. `part-1-phase-entry.md` — phase_entry/refinement.md + synthesis_workflow.md + embedded_rubric.md
2. `part-2-references.md` — fetchable reference catalog + stitched-output pointer block
3. `part-3-context.md` — embedded parent baseline + engine-flagged suspects + facts (with eviction if any)

The conditional parts (cross-service in codebase-content + env-content) MAY collapse to a smaller part or be omitted entirely when the predicate doesn't fire; numbering stays stable per kind because the index lists only the parts that exist.

### The part writer abstraction

```go
// partWriter accumulates content into the current part. When the
// current part's size approaches PerPartTokenCeiling, Flush()
// finalizes it and returns the next-part filename. The composer
// then begins a new part.
//
// Composers do NOT call Flush() arbitrarily — they call StartPart()
// at known semantic boundaries (e.g. between platform_principles
// and the cross-service block). The cap is a runtime check that
// fires only when a single semantic group exceeds the per-part
// budget; in normal operation, semantic boundaries control split.
type partWriter struct {
    parts []part            // (slug, body) pairs
    cur   *part             // currently-being-written part
}

func (w *partWriter) StartPart(slug string, description string) {
    if w.cur != nil { w.flush() }
    w.cur = &part{slug: slug, desc: description}
}

func (w *partWriter) Write(s string) error {
    // append to cur; if cur exceeds PerPartTokenCeiling and the
    // call would push it further, return an error so the composer
    // knows to call StartPart with a sub-slug. Composer authors
    // semantic boundaries; the runtime cap is the safety net.
}

func (w *partWriter) Persist(dir string) (indexPath string, err error)
```

Composer call shape:

```go
w := newPartWriter()
w.StartPart("phase-entry", "phase entry + synthesis workflow + citation guides")
w.WriteAtom("phase_entry/codebase-content.md")
w.WriteAtom("briefs/codebase-content/synthesis_workflow.md")
// ...
w.StartPart("platform", "platform principles")
w.WriteAtom("briefs/scaffold/platform_principles.md")
// ...
indexPath := w.Persist(briefsDir)
```

### Drop the old caps

`CodebaseContentBriefCap = 66 KB` and `EnvContentBriefCap = 56 KB` are gone. The replacement is `PartFileCap = 44 * 1024` + `PerPartTokenCeiling = 22000` (single per-part cap shared by all multi-file composers). `RefinementBriefCap = 80 * 1024` stays (refinement still streams facts most-recent-first; the budget arithmetic still wants a body-side cap to allocate within), but applies per-part now.

`BriefDiskFallbackThreshold = 40 * 1024` — kept. Still gates the inline-vs-pointer split for the small-brief kinds (scaffold, feature, finalize, claudemd-author) that don't multi-file. For multi-file kinds (codebase-content, env-content, refinement), the "inline" branch is dead — they always go to disk because they always emit ≥2 files.

`ReadToolTokenCeiling = 33000` — keep as the single-file ceiling for non-multi-file kinds. New `PerPartTokenCeiling = 22000` is the multi-file invariant.

### Dispatch wrapper teaching

Three phase-entry atoms (`codebase-content.md`, `env-content.md`, `refinement.md`) carry the dispatch contract. Today they say:

> Pointer (body > 40 KB) — `response.briefPath` is the absolute path to the engine-persisted brief on disk. Dispatch with a thin wrapper telling the sub-agent to `Read <briefPath>` first thing.

Update to:

> Pointer (multi-file, always for codebase-content / env-content / refinement) — `response.briefPath` is the absolute path to `index.md` under `<outputRoot>/.briefs/<kind>-<codebase>-<ts>/`. Index lists the part files in read order. Dispatch with a thin wrapper telling the sub-agent: "Read `<briefPath>` first; then Read each part file listed in its 'Read order' section before authoring." The brief carries the same content as before; it's split across files so each fits under the Read-tool's single-shot cap.

The phase-entry atoms (which the agent reads at phase-entry, before dispatch) MUST teach the read-the-index-then-the-parts contract.

### Test fixture refactor

Replace the synthetic-nil-extra-payload fixture in `briefs_token_ceiling_test.go` with a real-slug (nestjs-showcase) plan + 142-fact corpus mimicking run-31 steady state:

- 30 porter_change × 3 codebases (90 facts)
- 16 contract facts at plan scope
- 12 field_rationale × 3 codebases (36 facts)

Helpers live in the test file: `realShowcasePlan()` already exists; add `runShapedFactsCorpus()` that builds the 142-record list with proper `Scope` populated so `FilterByCodebase` returns non-empty per-codebase subsets.

The new tests pin per-part:

```go
TestBuildCodebaseContentBrief_MultiFile_RealSlug_PartsUnderCap
TestBuildEnvContentBrief_MultiFile_RealSlug_PartsUnderCap
TestRefinementBrief_MultiFile_RealSlug_PartsUnderCap
TestBriefDispatch_PointsAtIndex
TestBriefIndex_ListsAllPartsInReadOrder
```

The legacy `TestBrief_StaysUnderReadToolTokenCeiling` is split:
- For non-multi-file kinds (scaffold, feature, finalize, claudemd-author) the test stays — they remain single-file.
- For multi-file kinds the legacy test shape is replaced with the per-part cap assertion above.

### Phased rollout (RED → GREEN → REFACTOR)

**Phase 0 — this plan.**

**Phase 1 — RED.** Write the five new tests against the new shape. They MUST fail (the composer doesn't multi-file yet). Run `go test ./internal/recipe/... -run MultiFile` → expect failures.

**Phase 2 — GREEN.** Implement:
1. `partWriter` abstraction in `briefs.go` (or a new `briefs_parts.go` file).
2. New constants `PerPartTokenCeiling`, `PartFileCap`; doc-comment updates on `briefs.go` calling out the run-31 closure.
3. `BuildCodebaseContentBrief` rewrites `appendCodebaseContentAtoms` + the metadata/facts/parent-baseline/sibling sections to use `partWriter`.
4. `BuildEnvContentBrief` same shape.
5. `BuildRefinementBrief` same shape.
6. `Brief` struct gains `IndexPath string` (absolute path; populated for multi-file kinds) and `PartPaths []string` (absolute paths in read order; populated for multi-file kinds). When `IndexPath != ""`, `Body == ""` and the disk write happened during composition. The handler at `handlers.go::handleBuildSubagentPrompt` short-circuits the `len(prompt) > BriefDiskFallbackThreshold` branch for these kinds — the composer already wrote, the handler just plumbs `IndexPath` into `RecipeResult.BriefPath`.
7. Dispatch-wrapper template (the three phase-entry atoms) updated.

Run `go test ./internal/recipe/... ./internal/tools/... -run MultiFile` → all green.

Then `go test ./... -short` → no regressions.

Then `make lint-local` (or fast if local is too slow).

**Phase 3 — REFACTOR.** Drop the old caps (`CodebaseContentBriefCap`, `EnvContentBriefCap`). Update doc comments. Update `docs/zcprecipator3/system.md` §5 (knowledge channels) if the brief-shape narrative is documented there (it isn't currently — channels are described semantically, not by file count). Update CHANGELOG entry under "Run-31 Fix #1 closure (multi-file architecture)".

Final verification: `go test ./... -race`, `make lint-local`, all green.

## Scope guard

**Out of scope (separate F-track items):**
- Fix #4 (screenshot capture at close).
- F-45 (closure-of-expectation in tier README intros).
- Simulator multi-file plumbing (`sim/` package — different concern).

**In scope but care:**
- The `Brief` struct change ripples to callers in `internal/tools/`. Audit `grep -rn "\.Body\b" internal/tools/` and update any caller that formerly assumed `Body` was the full brief.
- Replay tooling at `cmd/zcp-recipe-sim/` reads composed briefs; either it consumes via `BuildSubagentPromptForReplay` (already plumbed) and the multi-file shape is invisible there because the index path comes through, or it has direct calls that need the same plumbing as the production handler.

## Open question — at decision time

If the part-split for `codebase-content` worker variant pushes any single part (e.g. `part-5-context.md` when the run records ~93 facts in scope) past `PerPartTokenCeiling`, the part writer's runtime cap fires and the composer needs to split that part further (`part-5a-context.md`, `part-5b-context.md`). The cleanest answer is: when the metadata + facts + parent baseline section exceeds the budget, split facts into their own part (`part-5-facts.md`) and keep the rest in `part-6-context.md`. Document this in the GREEN phase as the only split-during-composition path; semantic group splits stay author-controlled.
