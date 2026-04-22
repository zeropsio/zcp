# plans/v39-fix-stack.md — close content-emitter gaps + F-17-runtime

**Status**: TRANSIENT per CLAUDE.md §Source of Truth #4. Archive to `plans/archive/` after v39 verdict ships.
**Prerequisites**: [`../runs/v38/verdict.md`](../runs/v38/verdict.md), [`../runs/v38/CORRECTIONS.md`](../runs/v38/CORRECTIONS.md), [`../../spec-content-surfaces.md`](../../spec-content-surfaces.md).
**Target tag**: `v8.113.0` (or higher — depends on how many Cx land before cut).
**Estimated effort**: 5–7 days focused work. Cx-1 (GO-TEMPLATE-GROUND) is the multi-day one.

This doc is the execution plan the v39 implementor follows. Each Cx-commit carries: scope, files touched, RED-test name, acceptance criterion. Commits are dependency-ordered; items marked "parallel-safe" can land in any order relative to siblings.

---

## 0. Root-cause recap (from runs/v38)

v37 surfaced F-17: main agent paraphrased atoms when composing Task prompts. Cx-5 (v8.112.0) shipped `BuildSubagentBrief` + `VerifySubagentDispatch` — engine stitches the brief; verify-subagent-dispatch checks the Task prompt's SHA matches.

v38 surfaced three deeper issues (see [`../runs/v38/CORRECTIONS.md`](../runs/v38/CORRECTIONS.md)):

1. **F-17-runtime** — `VerifySubagentDispatch` is opt-in (no `PreToolUse` hook wires it against `Agent`). Main agent never called it. Editorial-review dispatch was 72% paraphrased (47,205 B → 13,229 B) and never caught.
2. **F-GO-TEMPLATE** — env-tier README prose at [`internal/workflow/recipe_templates.go`](../../../internal/workflow/recipe_templates.go) is hardcoded Go strings (`envAudience` / `envDiffFromPrevious` / `envPromotionPath` / `envOperationalConcerns` — all switch-on-envIndex). Never run through spec-content-surfaces.md tests. **4 of 6 editorial-review CRITs were direct hits on this source** (CRIT #1 fabricated "expanded toolchain", CRIT #3 factually wrong "Stage hits same DB", CRIT #2 mode-immutable contradiction, CRIT #5 missing NATS HA bullet). Main agent patched the rendered files on the mount; the Go source still ships these bugs.
3. **F-ENFORCEMENT-ASYMMETRY** — content-emitter coverage is inverted from content volume:
   - Writer sub-agent: 13 prose units, full 60KB spec brief.
   - Main agent zerops.yaml comments: ~40 blocks × 3 codebases, only one "Comment style" sub-section in recipe.md (no classification taxonomy, no citation map).
   - Main agent envComments: 72 prose blocks per run, same thin teaching.
   - Engine recipe_templates.go: root README + 6× env READMEs, zero spec teaching.

The entity producing the most content (main agent) has the thinnest teaching about what good content looks like. The entity that produces no content (writer brief teaching) is perfectly taught. **Volume-to-teaching is inverted.**

---

## 1. Goals of v39

1. **F-17-runtime closes** — main-agent paraphrase physically cannot reach a guarded sub-agent.
2. **F-GO-TEMPLATE closes** — engine-authored content goes through the same spec gate as writer-authored content.
3. **F-23 closes** — `ZCP_CONTENT_MANIFEST.json` reaches the deliverable tarball.
4. **F-ENFORCEMENT-ASYMMETRY narrows** — every content-emitting path has spec teaching at its authoring layer AND a runtime check at its emission point.
5. **Writer brief slims from ~60KB to ~25KB** — moving classification/routing from prompt-carry to runtime-lookup + dropping wrong-role atoms.
6. **Convergence** — v39 should reach close-step in ≤1 editorial-review round (v38 needed 4), ≤1 finalize round (v38 needed 2).

If v39 clears these, **C-15 (recipe.md deletion) becomes the next unblocker** — see [`../PLAN.md`](../PLAN.md) §2 R2-R7.

---

## 2. The 10-Cx stack

Ordered by dependency. Each has a scope, files-touched list, RED test name, and acceptance criterion.

### Cx-1 — GO-TEMPLATE-GROUND (F-GO-TEMPLATE close — headline fix)

**Scope**: reshape the hardcoded prose-generating functions in `recipe_templates.go` so every claim is computed from plan data instead of hardcoded per envIndex. Add a gold-test that runs the spec's per-surface single-question tests against `BuildFinalizeOutput(fixturePlan)` output. Fabrication becomes structurally impossible because the functions have no construction path for un-backed claims.

**Why now**: 4 of 6 editorial-review CRITs in v38 came from this source. Fixing it removes the single largest class of content defect. Dependency-free.

**Files touched**:

- `internal/workflow/recipe.go` — add `EnvTemplate` struct carrying per-env: `RuntimeMinContainers`, `RuntimeMode` (HA/NON_HA/absent), `DBMode`, `CacheMode`, `QueueMode`, `BackupPolicy` (bool), `ReadinessCheckTightened` (bool). Populated during provision step from the plan's env tier metadata. Added as `plan.EnvTemplates [6]EnvTemplate`.
- `internal/workflow/recipe_templates.go`:
  - `envAudience(envIndex, plan)` — rewrite to compose from `plan.EnvTemplates[envIndex]`. Each bullet derived from a plan field. No bullet emitted if the field doesn't differ from neighboring tiers.
  - `envDiffFromPrevious(envIndex, plan)` — iterate `EnvTemplates[envIndex-1]` vs `EnvTemplates[envIndex]`. For each field that differs, emit one bullet. Identical fields produce no bullet (CRIT #1 fix: both nodejs@24 → no "expanded toolchain" bullet).
  - `envPromotionPath(envIndex, plan)` — compose from forward-diff.
  - `envOperationalConcerns(envIndex, plan)` — compose from env-specific flags.
- `internal/workflow/recipe_templates_test.go` — add `TestFinalizeOutput_PassesSurfaceContractTests`:
  - Table-driven: 2 fixture plans (showcase + minimal).
  - For each env in each plan, render `GenerateEnvREADME(plan, i)`.
  - Run each Surface 2 single-question test (from [`docs/spec-content-surfaces.md`](../../spec-content-surfaces.md) §"Per-surface test cheatsheet") as an assertion:
    - Every "What changes" bullet matches a field difference in the fixture's `EnvTemplates` data.
    - Every "Who this is for" bullet corresponds to a plan flag that actually distinguishes this tier.
    - No bullet mentions a service type (`nodejs@24`, `postgresql@18`) that doesn't appear in the env's import.yaml.
    - No bullet claims a "mode" / "scale" / "HA" property that contradicts the env's import.yaml.
  - Test fails with the specific unbacked bullet + fixture path.

**RED tests**:

- `TestFinalizeOutput_PassesSurfaceContractTests` (must fail against current `recipe_templates.go:273, 345, 382`; pass after refactor).
- `TestEnvDiffFromPrevious_NoHardcodedClaims` — generic lint: scan the function body via `go/ast`, assert all return strings contain only `{{.Field}}` template markers OR references to `plan.EnvTemplates[...]` fields. Any raw string literal with platform-domain vocabulary (`nodejs`, `HA`, `CDE`, `SSH`) fails. Prevents regression.

**Green**: both tests pass against rewritten functions. All existing tests in `recipe_templates_test.go` continue to pass (the 2 fixture plans need to produce the same rendered output modulo the removed fabricated bullets).

**Acceptance on v39**: editorial-review dispatch reports 0 CRITs on any env README (`1 — Remote (CDE)`, `3 — Stage`, `4 — Small Production`). Retry-cycle for `close/editorial-review` closes on first attempt.

**Estimated**: 2–3 days. Breakdown:
- **Day 1 — bullet audit (substantive design)**. Inventory every hardcoded bullet in the four functions (~40-50 bullets). Classify each as: (a) **plan-backed** — field exists in current `writeSingleService` yaml generator; (b) **prose-only** — bullet makes a claim no yaml field backs (e.g. "Backups become meaningful at this tier"); (c) **system-invariant** — true of every recipe by construction (e.g. "Each tier declares a distinct project.name"). For (b) bullets, user decides per-bullet: promote to schema + add yaml-generator support, or delete the bullet. Skipping this audit causes Cx-1 to either lose teaching (deleted (b) bullets) or move fabrication into the schema (unbacked schema fields).
- **Day 2 — refactor**. Extract `EnvTemplate` struct. Rewrite the four prose functions to compose bullets from struct fields (for (a) bullets) + an enumerated system-invariant bullet template list (for (c) bullets). Drop or add yaml-gen support for (b) bullets per the audit decisions.
- **Day 3 — gold test with three-way equality**. `TestFinalizeOutput_PassesSurfaceContractTests` asserts for each generated bullet: (schema field value) == (yaml-generator emitted text for that field) == (prose bullet's claim about that field). Three-way equality catches the case where a bullet's schema field is set but the yaml generator doesn't emit the corresponding config — the "fabrication moved into schema" failure mode.

---

### Cx-2 — MANIFEST-EXPORT-EXTEND (F-23 close)

**Scope**: extend the export root-file whitelist at [`internal/sync/export.go:236`](../../../internal/sync/export.go#L236) to include `ZCP_CONTENT_MANIFEST.json` (and `*.json` / `*.md` root files by pattern for future-proof).

**Why now**: one-line fix; unblocks the manifest reaching the deliverable. Dependency-free. Parallel-safe.

**Files touched**:

- `internal/sync/export.go` — change L236 from `[]string{"TIMELINE.md", "README.md"}` to `[]string{"TIMELINE.md", "README.md", "ZCP_CONTENT_MANIFEST.json"}`. Optionally accept a `rootFilePattern` config so future overlays don't require another export edit.
- `internal/sync/export_test.go` — add `TestExportRecipe_IncludesRootManifest`:
  - Setup temp dir with `TIMELINE.md`, `README.md`, `ZCP_CONTENT_MANIFEST.json` (valid JSON), and one stray root-level file.
  - Call `ExportRecipe`.
  - Extract archive; assert `TIMELINE.md`, `README.md`, `ZCP_CONTENT_MANIFEST.json` present; stray file NOT present (pattern is explicit, not wildcard).

**RED test**: `TestExportRecipe_IncludesRootManifest` fails at HEAD (manifest not in archive).

**Green**: test passes after whitelist extension.

**Acceptance on v39**: `find nestjs-showcase-v39/ -name "ZCP_CONTENT_MANIFEST.json"` returns a file.

**Estimated**: 30 minutes.

---

### Cx-3 — ROUTETO-RECORD-ENFORCE (enable thin writer — Phase 1)

**Scope**: `zerops_record_fact` validates `routeTo` server-side at call time against the same classification×surface routing matrix currently taught in the writer brief (atoms `routing-matrix.md` + `classification-taxonomy.md`). Without a valid `routeTo`, the call fails with a specific remediation naming which values are legal for the given `type`.

**Why now**: v38 session logs show 45+ recorded facts, only 3 with `scope` set, 0 with `routeTo`. The "classify at record time" design intent (per [`fact-recording-discipline.md`](../../../internal/content/workflows/recipe/principles/fact-recording-discipline.md) §"The recording step IS the classification moment") is advisory-only. Making it mandatory shifts classification load from the writer to the agent that has the failure mode in front of it. Prerequisite for Cx-6 (writer brief slim).

**Files touched**:

- `internal/workflow/fact_record.go` (or wherever `handleRecordFact` lives):
  - Add `routeTo` as a required field in `RecordFactInput`.
  - Validate against `ClassToRoutingCells(type)` — the class-to-surface matrix.
  - On invalid input, return `INVALID_PARAMETER` with remediation listing the legal `routeTo` values.
- `internal/workflow/classification.go` (new) — extract the routing matrix from the two writer-brief atoms into Go data. Single source of truth that both the `record_fact` validator AND a new `classify` action (Cx-4) consume.
- Write unit tests for every classification×surface cell.
- Atom update — `fact-recording-discipline.md` now says "routeTo is required; omitting it fails the call; legal values per type in the API error message".

**RED tests**:

- `TestRecordFact_RejectsMissingRouteTo`
- `TestRecordFact_RejectsInvalidRouteToForType` (e.g. `type=gotcha_candidate, routeTo=claude_md` requires override_reason)
- `TestRecordFact_AcceptsValidRouteTo`

**Green**: all three pass.

**Acceptance on v39**: main-session.jsonl contains ≥10 `zerops_record_fact` calls, 100% of which carry a non-empty `routeTo`.

**Estimated**: 1 day.

---

### Cx-4 — CLASSIFY-RUNTIME-ACTION (enable thin writer — Phase 2)

**Scope**: new engine action `zerops_workflow action=classify` that takes `{type, title, mechanism, citation_topic}` and returns `{routeTo, violations[]}`. Lets the writer classify individual items at decision time without loading the full classification table in its brief.

**Why now**: depends on Cx-3 (shared classification.go). Unlocks Cx-6 writer brief slim.

**Files touched**:

- `internal/tools/workflow.go` — add `action=classify` handler.
- `internal/workflow/classification.go` — add `Classify(input ClassifyInput) ClassifyResult` that applies the matrix + citation-map rules.
- Tests: 6 cases, one per classification class, each asserting the right routeTo + any violations.

**RED test**: `TestClassifyAction_ReturnsRouteToWithCitationCheck` — if `citation_topic` is on the Citation Map and `citation` field is empty, return violation `folk_doctrine_candidate`.

**Green**: tests pass.

**Acceptance on v39**: writer sub-agent log shows it calls `classify` for any ambiguous item (vs. loading the full taxonomy in its brief).

**Estimated**: 4 hours.

---

### Cx-5 — WRITER-SELF-REVIEW-AS-STEP-GATE

**Scope**: move the bash checks currently in the `self-review-per-surface.md` atom (manifest exists, manifest valid JSON, no folk-doctrine bullets, per-codebase fragments present, CLAUDE.md byte floor) to engine-enforced step-gate at `action=complete substep=readmes`. Writer cannot attest completion until every machine check passes.

**Why now**: self-review as advisory atom text produced 2 writer CRITs in v38 (folk-doctrine + wrong-surface). Moving the checks to the engine step-gate makes them non-bypassable.

**Files touched**:

- `internal/workflow/recipe_step_checks.go` (or equivalent) — add a new set of checks that fire specifically at `deploy/readmes` substep completion:
  - `readmes_manifest_exists_and_valid_json`
  - `readmes_no_folk_doctrine` — every gotcha whose topic is on the Citation Map AND whose body contains no `[topic:...]` reference fails.
  - `readmes_no_wrong_surface_gotcha` — every manifest item with `routeTo=claude_md` is NOT present in any README knowledge-base fragment.
  - `readmes_cross_readme_uniqueness` — existing check.
  - `readmes_claude_md_byte_floor` — existing check, extended with case-insensitive base-sections match (fixes v38 harness false-positive on lowercase "Dev loop").
- Writer atom `self-review-per-surface.md` shrinks from 4KB advisory bash to ~500B "on completion the engine runs these checks; fix the rendered files and retry".

**RED tests**: one per check — fixture a deliberately-bad manifest + README, assert each check fires with the specific offending line.

**Green**: tests pass; existing `fragment_*` checks continue to pass.

**Acceptance on v39**: writer-first-pass compliance failure count ≤ 2 (v38 had 9). Writer cannot attest `complete substep=readmes` on content that would fail editorial-review's folk-doctrine or wrong-surface tests.

**Estimated**: 1 day.

---

### Cx-6 — WRITER-BRIEF-SLIM

**Scope**: drop three atoms from the writer's dispatch brief; replace with runtime lookups or step-gate signals.

- Remove `principles.fact-recording-discipline` from `writerPrinciples()` — wrong role (writer consumes facts; doesn't record).
- Remove `briefs.writer.classification-taxonomy` + `briefs.writer.routing-matrix` from `writerBriefBodyAtomIDs()` — move to runtime lookup via Cx-4 `action=classify`.
- Move `briefs.writer.self-review-per-surface` content to a step-gate signal (Cx-5); retain a 500-byte stub pointing at the gate checks.

Target brief size: ~25KB (from current ~60KB). All removed teaching stays in the system as engine-side enforcement or runtime action.

**Why now**: depends on Cx-3, Cx-4, Cx-5. With enforcement moved to the right layers, the brief shrinks naturally.

**Files touched**:

- `internal/workflow/atom_stitcher.go` — update `writerBriefBodyAtomIDs()` + `writerPrinciples()`.
- `internal/workflow/subagent_brief_test.go` — add `TestBuildWriterBrief_BriefSizeUnder30KB` + assert removed atoms absent from output.
- Atom files — `classification-taxonomy.md` and `routing-matrix.md` retained but flagged as "runtime lookup source" in their frontmatter.

**RED tests**:

- `TestBuildWriterBrief_BriefSizeUnder30KB` — assert brief ≤ 30,000 bytes.
- `TestBuildWriterBrief_NoFactRecordingDiscipline` — assert content missing.
- `TestBuildWriterBrief_NoFullClassificationTable` — assert the full routing matrix table is NOT present (small class reference allowed, not full table).

**Green**: tests pass. All downstream behavior tests in `v38_dispatch_integrity_test.go`-equivalent still pass against the smaller brief for byte-identical shape.

**Acceptance on v39**: `BuildSubagentBrief(plan, writer, ...).Prompt` size under 30KB.

**Estimated**: 4 hours.

---

### Cx-7 — MAIN-AGENT-COMMENT-TEACHING + VOICE-CHECK

**Scope**: add a `zerops_knowledge topic=comment-style` lookup the main agent fetches before writing zerops.yaml or envComments. Add deploy-step + finalize-step voice checks that grep for first-person/journal patterns in comment lines.

**Why now**: main agent emits ~40 zerops.yaml comment blocks × 3 codebases + 72 envComment blocks per run. Current teaching is one sub-section of `recipe.md`. No check fires on voice quality. User's remembered pattern ("I started dev server it failed...") is exactly this class. Parallel-safe with Cx-1/2/3/4/5/6.

**Files touched**:

- `internal/content/topics/comment-style.md` (new) — lift + expand the writer brief's `comment-style` principle atom for main-agent use. Add the spec §13 counter-examples (journal voice, "turns out", self-inflicted-as-gotcha).
- `internal/workflow/recipe_checks.go`:
  - `zerops_yaml_comment_voice` — deploy-step check, regex `\b(I |we |turns out|then I|tried [a-z]+ failed|worked)\b` in comment lines of `*/zerops.yaml`. Fails with specific file:line.
  - `env_comment_voice` — finalize-step equivalent scanning envComments in generated import.yaml comment blocks.
- Update `recipe.md` generate + finalize step-entry text to point at `comment-style` topic.

**RED tests**: one per check + one per fixture bad-voice comment.

**Green**: tests pass.

**Acceptance on v39**: zero `*_comment_voice` failures across 3 zerops.yaml + 6 env import.yaml files.

**Estimated**: 4 hours.

---

### Cx-8 — ENV-COMMENT-SCAFFOLD-DECISION-CHECK

**Scope**: finalize-step check that env import.yaml service comments don't contain scaffold-decision teaching that belongs in the per-codebase `zerops.yaml` comment instead.

**Why now**: addresses the spec §11 counter-example class "Scaffold decisions disguised as gotchas" transposed to env comments. E.g. a tier 0 env comment that describes "how our NestJS app wires TypeORM" belongs in apidev/zerops.yaml, not env 0. Parallel-safe.

**Files touched**:

- `internal/workflow/recipe_checks.go` — `env_comment_scaffold_decision_placement`. Heuristic: comment mentions a framework-specific concept (list: `TypeORM`, `Nest`, `Svelte`, `Vite`, `SvelteKit`) AND isn't naming a platform mechanism. Fails with remediation "move to apidev/zerops.yaml comment or discard; env comments explain tier decisions, not code shape."
- Test + fixture.

**RED**: fixture env comment mentioning `TypeORM` fails.

**Green**: fixture with tier-decision-only passes.

**Acceptance on v39**: zero `env_comment_scaffold_decision_placement` failures.

**Estimated**: 3 hours.

---

### Cx-9 — STRIPPING-VISIBILITY-WARN

**Scope**: when main-agent Edit patches a file whose content was originally emitted by a Go-source function (recipe_templates.go), log a stderr warning naming the originating function. Makes the "strip from mount without fixing source" pattern visible.

**Why now**: v38 main agent patched 5 env READMEs + 1 app README + 2 manifests to satisfy editorial-review — all stripping. Without visibility, this pattern recurs silently. Parallel-safe.

**Files touched**:

- `internal/workflow/recipe_overlay.go` — when `OverlayRealREADMEs` detects that a file on the mount differs from what `Generate*README(plan)` would produce, log the divergence. Not a failure — a warning that the Go source should be updated.
- Alternative: the commit hook `.githooks/pre-commit` (new) greps for recent Edit operations on recipe-output files and warns if the corresponding Go-source function hasn't changed in the same commit.

**RED test**: warn-on-divergence logic has a unit test.

**Green**: warning fires in a synthetic "patched rendered but source unchanged" scenario.

**Acceptance on v39**: when editorial-review fires CRITs and main agent patches, session-end summary lists "N patches applied to files also authored by Go source; consider upstream fix".

**Estimated**: 3 hours.

---

### Cx-10 — SURFACE-DOC-COMMENT-LINT

**Scope**: build-time lint under `tools/lint/surface_coverage.go` that walks every exported function in `internal/workflow/recipe_templates.go` + any new emitter, requires a `// Surface: N / Test: <spec-single-question>` doc comment, and verifies a corresponding test exists.

**Why now**: systemic prevention. Any future content-emitter added without a spec test fails CI. Parallel-safe. Seals the class v38 exposed.

**Files touched**:

- `tools/lint/surface_coverage.go` (new) — `go/ast` walker + existence check on `_test.go` file.
- `Makefile` — add `lint-surface-coverage` target to `lint-local`.
- Existing emitters get their doc comment added as part of Cx-1.

**RED test**: remove a doc comment from one emitter, assert lint fails.

**Green**: lint passes with all current emitters documented.

**Acceptance on v39**: `make lint-local` passes.

**Estimated**: 4 hours.

---

### Cx-11 — DISPATCH-GUARD-AUTO-ENFORCE (F-17-runtime close)

**Scope**: fire `VerifySubagentDispatch` automatically before any `Agent` dispatch whose description matches a guarded-role keyword. Not as a Claude Code `PreToolUse` hook (which Claude Code owns and we can't touch) — as a retroactive engine check at `action=complete substep=readmes` (and close-phase equivalents): the engine scans the main-session for recent `Agent` tool calls whose descriptions match the guarded keywords, hashes each submitted prompt, and fails the step-complete call with `SUBAGENT_MISUSE` if any prompt SHA doesn't match the most-recent `build-subagent-brief` result for that role.

**Why now**: v38 proved the opt-in guard isn't invoked. Retroactive check doesn't prevent the paraphrase but blocks step-completion — so the next cycle must correct the dispatch. Rule-of-last-resort.

**Files touched**:

- `internal/workflow/recipe_step_checks.go` — add check `readmes_dispatch_integrity` fired at `complete substep=readmes` and `close/editorial-review` / `close/code-review` attest. Reads the session-scoped `LastSubagentBrief` + scans main-session.jsonl for recent Agent dispatches.
- Requires exposing recent Agent tool-use history to engine — may need a MCP callback or a main-session.jsonl path exposed to engine at step-complete time.
- Tests using the v38 deliverable's main-session.jsonl as fixture: assert editorial-review dispatch (L753, prompt 13229 B) fails the integrity check because its SHA doesn't match the engine-side `build-subagent-brief role=editorial-review` result.

**RED test**: `TestDispatchIntegrity_CatchesParaphrasedEditorialReview` using v38 fixture.

**Green**: test fails at HEAD (no check implemented); passes after implementation. Fixture shows `close/editorial-review` complete would have been blocked.

**Acceptance on v39**: zero dispatch-integrity failures in v39 session log. Alternatively, if main-agent does paraphrase, the step-complete fails loudly with `SUBAGENT_MISUSE` remediation.

**Estimated**: 1–2 days (hardest because the engine needs access to main-agent Agent history, which crosses Claude Code's abstraction).

---

## 3. Parallelization

| Cx | Depends on | Parallel-safe with |
|---|---|---|
| 1 GO-TEMPLATE-GROUND | — | 2, 7, 9, 10, 11 |
| 2 MANIFEST-EXPORT-EXTEND | — | any |
| 3 ROUTETO-RECORD-ENFORCE | — | 1, 2, 7, 9, 10 |
| 4 CLASSIFY-RUNTIME-ACTION | 3 | 1, 2, 5, 7, 9, 10 |
| 5 WRITER-SELF-REVIEW-AS-STEP-GATE | — | any |
| 6 WRITER-BRIEF-SLIM | 3, 4, 5 | 1, 2, 7, 9, 10 |
| 7 MAIN-AGENT-COMMENT-TEACHING | — | any |
| 8 ENV-COMMENT-SCAFFOLD-DECISION | — | any |
| 9 STRIPPING-VISIBILITY-WARN | — | any |
| 10 SURFACE-DOC-COMMENT-LINT | 1 (for initial doc comments) | 2, 3, 4, 5, 7, 9, 11 |
| 11 DISPATCH-GUARD-AUTO-ENFORCE | — | any |

One-person drive: 5–7 days sequential. Two-person parallel: 3–4 days.

---

## 4. Retrospective harness run (before v8.113.0 tag)

Per the v38 pattern: before tagging, run `zcp analyze recipe-run` retrospectively against the v38 deliverable. Expected behavior: same defect set visible (deliverable unchanged), but the v39 additions surface as NEW failures on the v38 tree:

- Cx-1 addition: `TestFinalizeOutput_PassesSurfaceContractTests` fails on v38 (the CRIT #1 "expanded toolchain" fails the spec test).
- Cx-5 addition: `readmes_no_folk_doctrine` flags v38's "benign zcli warning" retroactively.
- Cx-7 addition: `zerops_yaml_comment_voice` passes on v38 (comments are clean).
- Cx-11 addition: dispatch-integrity check fails v38's editorial-review SHA mismatch.

If the retrospective doesn't fail the v38 deliverable on the expected new checks, the checks don't work.

---

## 5. v39 commission spec

Same as v38 except:

```
TIER:                 showcase
SLUG:                 nestjs-showcase
FRAMEWORK:            nestjs
TAG:                  v8.113.0 (or higher — fill in post-release)
MUST_REACH:           close-step complete + ZCP_CONTENT_MANIFEST.json in deliverable
MUST_PASS:            all dispatch-integrity checks at close-phase attest
MUST_PASS:            all 3 guarded roles' dispatches byte-identical to engine brief (modulo trailing-newline)
MUST_PASS:            zero editorial-review CRITs on first pass
MUST_PASS:            readmes retry rounds ≤ 1, finalize rounds ≤ 1
```

**During-run tripwires** (new vs v38):

- Any `zerops_record_fact` call without `routeTo` → engine refuses. Monitor for refusal count.
- Any `Write` to env README / root README that doesn't correspond to an engine regeneration → stripping warning fires.
- Any `close/editorial-review` attest attempt with a paraphrased dispatch → fails `readmes_dispatch_integrity`.

---

## 6. What v39 is NOT doing

- C-15 recipe.md deletion (R2-R7 per PLAN.md §2) — deferred to post-v39.
- Framework diversity — v39 stays on nestjs-showcase.
- Minimal tier — independent.
- Publish-pipeline — post-v39.

---

## 7. Exit criteria

Plan is complete when:

- [ ] Cx-1–Cx-11 merged to main with green RED→GREEN cycle each.
- [ ] `go test ./... -race -count=1` green.
- [ ] `make lint-local` green including new `surface_coverage` lint.
- [ ] Retrospective harness run against v38 shows the new checks surface the v38 defects.
- [ ] v8.113.0 (or bumped) tagged + pushed.
- [ ] Slot block in [`HANDOFF-to-I10-v39-prep.md`](../HANDOFF-to-I10-v39-prep.md) filled with commit SHAs.
- [ ] User commissioned v39.
- [ ] `runs/v39/{machine-report.json, verification-checklist.md, verdict.md}` all present; verify-verdict hook passes.
- [ ] Verdict decision shipped (PROCEED / ACCEPT-WITH-FOLLOW-UP / PAUSE / ROLLBACK-Cx).

If v39 verdict is PROCEED, the next handoff targets C-15 (recipe.md deletion per PLAN.md §3 step-entry migration). If PAUSE, the next handoff targets whichever new layer v39 exposed.
