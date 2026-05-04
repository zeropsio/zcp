# Develop response atom proliferation — firehose v2

**Status:** deferred — needs design + Codex pressure-test before plan.
**Source:** Investigation of post-Phase-3 friction (suite `20260503-173119`,
3 of 4 retros still flag "wall of guidance, 2k+ lines").

## What

After the Phase 3 atom retag (12 atoms tagged with `runtimes:` axis), the
develop-step response is still flagged as a "firehose" by 3 of 4 latest
eval agents. The hypothesis going in was that per-runtime knowledge
briefings (e.g., DB-shaped Go recipe in `internal/knowledge/recipes/go-hello-world.md`)
were over-comprehensive. **That hypothesis was REFUTED by investigation.**

## Real root cause (from investigation)

Knowledge briefings (`internal/knowledge/recipes/*.md`) are NOT in the
develop response. Agents must explicitly call `zerops_knowledge` for them.
The 2k+ line firehose comes entirely from develop **atoms**.

**Numbers (verified):**
- 86 total atoms in `internal/content/atoms/`
- 52 atoms tagged `develop-active` phase
- 1742 total lines across the 52 develop-active atoms
- Phase 3 retag narrowed 12 atoms by `runtimes:` axis (good)
- Remaining proliferation comes from:
  - **Close-mode atom proliferation**: 14+ atoms covering every (mode × close-mode × environment × strategy) combination. Examples: `develop-close-mode-auto-dev`, `develop-close-mode-auto-standard`, `develop-close-mode-auto-workflow-dev`, `develop-close-mode-auto-workflow-simple`, `develop-standard-unset-iterate`, `develop-standard-unset-promote-stage`, etc. Each is small, but they pile up.
  - **Service-agnostic atoms**: many atoms have NO per-service axes (no `runtimes`, no `modes`, no `closeDeployModes`). They render once per envelope regardless of plan shape. Examples: `develop-platform-rules-common`, `develop-verify-matrix`, `develop-dev-server-reason-codes`, `develop-env-var-channels`. Each adds 30-60 lines.
  - **Mode-specific atoms still verbose**: atoms that DO declare `modes: [X]` still carry their full content when they fire. No internal sub-section filtering by current state.

## What's NOT the cause

- Knowledge briefings (recipes) — not in develop response.
- Per-runtime knowledge — separate tool, separate friction class.
- Phase 3 retag was the right move and works for what it targeted.

## Investigation findings

**Composition (from `workflow_develop.go::renderDevelopBriefing`)**:
1. `LoadAtomCorpus()` (line 224) — all 86 atoms.
2. `Synthesize(envelope, corpus)` (line 231) — filters by phase, environment, runtimes, modes, deployStates, etc.
3. `RenderStatus(...Guidance: BodiesOf(matches)...)` (line 239) — concatenates atom bodies with `\n\n---\n\n`.

**Filtering machinery works correctly**: phase ✓, environment ✓, runtimes (when declared) ✓, modes (when declared) ✓.

**What's missing**:
- No "narrow service-agnostic atoms by deps presence" filter.
  - Example: `develop-env-var-channels` always renders — even for plans with no managed dependencies.
- No "narrow service-agnostic atoms by scope size" filter.
  - Example: multi-service-aggregation atoms render even when scope = 1 service.
- No subsection-level filtering inside atoms.
  - Example: `develop-platform-rules-container` covers dynamic dev-server lifecycle PLUS general container rules. For a static-only scope, the dev-server section is noise.

## Boundary check

- **Workflow-team owns**: `internal/content/atoms/` (atom content), `internal/workflow/synthesize.go` (filter machinery).
- **No recipe-team boundary issue** — knowledge briefings are not the cause. This is purely workflow-team work.

## Fix shapes (for future plan, escalating cost)

### (a) Audit + consolidate close-mode atoms — cheapest

14+ atoms with similar shape per (mode × close-mode × strategy). Could be:
- Merged into 3-4 super-atoms with clearer mode-dispatch sections.
- Or kept separate but trimmed (each ~20% shorter).

Risk: heavy snapshot churn. Test via flow-eval before/after.

### (b) Add `dependencies:` axis filter — small structural

New atom-frontmatter axis: `dependencies: [present|absent]` or `dependencies: [postgresql, valkey]`.
Atoms talking about managed-dep wiring would gate on `dependencies: [present]` and only fire
when plan declares deps. Atoms about no-deps cases would gate on `dependencies: [absent]`.

Cost: ~3-5 atoms with explicit deps gate. Synthesize filter extension. Lint extension to enforce
authoring rule.

### (c) Add `scope-size:` axis filter — small structural

For atoms that only matter in multi-service scope (cross-deploy, scope ≥ 2). Gate them via
`scopeSize: [multi]`.

Cost: similar to (b), narrower applicability.

### (d) Atom subsection filtering — biggest structural

Rework atom format to allow `{{#if runtimes contains dynamic}}...{{/if}}` blocks.
Cost: large — atom format change, parser change, lint change, full corpus re-author.

**Codex should pressure-test which (a)/(b)/(c)/(d) is correct, or whether a 5th
option (split-by-runtime-class atoms) wins.**

## Trigger to promote

Promote when:
1. We have appetite for ~1-2 days of corpus + filter work.
2. Eval data persists showing >50% of retros flagging firehose post-Phase-3.
3. Codex confirms the boundary (no recipe-team coordination needed).

## Risks

- Heavy golden-snapshot churn — must regenerate via `ZCP_UPDATE_ATOM_GOLDENS=1`
  and review diffs carefully (high risk of removing actually-needed content).
- Splitting atoms further (option a) without trimming content moves the firehose
  laterally but doesn't shrink it.
- Adding new axes (b/c) requires lint enforcement so authors actually use them.

## What this is NOT a fix for

- The "Go knowledge guide is built around DB-backed app" friction in retro —
  that's about `zerops_knowledge` tool returns, not develop response. Separate concern.
- Tool batching friction — addressed in `plans/flow-eval-followup-fixes-2026-05-03.md`.
- 502 warning misinterpretation — addressed in same plan.
