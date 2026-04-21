# rollout-sequence.md — commit-level plan for cleanroom migration

**Purpose**: commit-level plan for landing zcprecipator2 under the **cleanroom** candidate chosen in [`migration-proposal.md §6`](migration-proposal.md). One commit per row; each row is individually revertable; each row has explicit test coverage at the CLAUDE.md testing layers (unit / tool / integration / e2e); each row names its ordering dependencies and the consequences of shipping alone.

Scope anchors (per [`migration-proposal.md §1`](migration-proposal.md)):
- Operational substrate untouched — no commit in this sequence modifies `internal/platform/*`, `internal/ops/*` (except facts log `RouteTo` field), MCP tool bodies, SSH boundary, workflow state machine, dev-server spawn / pkill / port-stop, env-README Go templates.
- Content-tree: `internal/content/workflows/recipe.md` deleted; `internal/content/workflows/recipe/` 86-atom tree added.
- Go-layer: `recipe_topic_registry.go` replaced by `atom_manifest.go`; `recipe_guidance.go` stitcher body rewritten; `symbol_contract.go` added; `recipe_plan.go` extended; `workflow_checks_*.go` = 56 keep + 16 rewrite + 1 delete + 5 new = 78 total; `cmd/zcp/check/` adds 16 shim subcommands.
- Schema: `FactRecord.RouteTo` added (additive); `StepCheck` payload shape changed (breaking: drops `ReadSurface/Required/Actual/CoupledWith/HowToFix/PerturbsChecks`; adds `PreAttestCmd` + `ExpectedExit`); close `NextSteps[]` defaulted empty.

Conventions:
- **C-N** = commit N in the sequence
- **Breaks alone** = what regresses if this commit ships without its successors
- **Ordering deps** = which prior commits must exist
- **Test layers** = which CLAUDE.md testing-layer tests exercise the change: `unit` (platform/auth/ops/), `tool` (tools/), `integration` (integration/), `e2e` (e2e/ -tags e2e), `build-lint` (CI-time grep/lint guards)

Target sequence: **15 commits**. Expected wall: 3–4 days of focused work if each commit is reviewed before the next lands. Each commit must pass `make lint-local` + `go test ./... -count=1` before the next commit is written (CLAUDE.md TDD rule: RED → GREEN → REFACTOR; behavioral commits land with failing tests first, then passing).

---

## 0. Ordering overview

```
C-0    baseline: substrate-invariant regression tests           [test-only, safe]
C-1    add SymbolContract plan field + derivation               [additive, dead]
C-2    add FactRecord.RouteTo + manifest schema ext              [additive, dead]
C-3    scaffold atom_manifest.go (unused)                        [additive, dead]
C-4    land 96 atom files under recipe/                          [additive, dead]
        (86 original + 10 editorial-review; refinement 2026-04-20)
C-5    rewrite recipe_guidance.go stitchers                      [*** cutover ***]
C-6    add 5 new architecture-level checks                       [gate-additive]
C-7    rewrite 16 checks to runnable + add zcp check CLI shim    [gate-refactor]
C-7.5  add editorial-review role (sub-agent + substep + checks)  [role-additive; refinement 2026-04-20]
C-8    expand writer_manifest_honesty to all routing dims (P5)   [gate-stricter]
C-9    delete knowledge_base_exceeds_predecessor                 [gate-smaller]
C-10   shrink StepCheck failure payload (drop verbose fields)    [breaking shape]
C-11   empty NextSteps[] at close completion                     [server config]
C-12   land docs/zcprecipator2/DISPATCH.md                       [docs-only]
C-13   build-time lints on recipe/ tree                          [CI gates]
C-14   v35 dry-run infrastructure + calibration-bar scripts      [test surface]
C-15   delete recipe.md + recipe_topic_registry.go               [old removal]
```

Each commit is further described below.

---

## C-0 — Baseline: operational substrate untouched verification

**Purpose**: before any rewrite commit, establish a green CI baseline covering every substrate invariant that v34 validated as pristine. Any subsequent commit that regresses this baseline is detected immediately.

**What changes**:
- `internal/platform/ssh_test.go` — add regression tests covering: SSH boundary held under every emission path (no `cd /var/www/{host} && <exec>` bash emission; `bash_guard` middleware rejects attempts) [v17/v8.80 invariants]
- `internal/tools/workflow_subagent_misuse_test.go` — `SUBAGENT_MISUSE` returned when any sub-agent context calls `zerops_workflow action=start` [v8.90 invariant]
- `internal/workflow/git_config_mount_test.go` — single container-side `ssh {hostname} "git config + git init + initial commit"` call shape [v8.93.1 invariant]
- `internal/ops/facts_log_test.go` — `FactRecord.Scope` enum values + filter behavior [v8.96 Theme B invariant]
- `internal/workflow/recipe_templates_test.go` — env-README Go-template outputs match YAML-ground-truth byte-for-byte for the six canonical env tiers (dev / stage / prod / HA / shared / worker) [v8.95 Fix B invariant]
- `internal/tools/workflow_read_before_edit_test.go` — Edit tool rejects edits to files the current session hasn't read [v8.97 Fix 3 invariant]
- `internal/sync/export_test.go` — `ExportRecipe` refuses when close step incomplete [v8.97 Fix 1 invariant]
- `internal/tools/workflow_close_next_steps_test.go` — current (pre-rewrite) close response has `NextSteps=[…export/publish hints]`; test captures current behavior so C-11's change is deliberate

**Rough LoC**: +400 lines (test-only)

**Test layers**: unit + tool

**Breaks alone**: nothing — test-only commit

**Ordering deps**: none

**Rationale for leading**: [CLAUDE.md](../../../CLAUDE.md) "TDD mandatory" rule + "Change impact — tests FIRST at ALL affected layers" + the substrate-preservation promise in [migration-proposal.md §1.1](migration-proposal.md). We cannot claim substrate is untouched without tests that fail if it moves. This commit establishes the safety net.

---

## C-1 — Add SymbolContract plan field + derivation helper

**Purpose**: introduce `plan.Research.SymbolContract` as an additive plan field with a derivation helper. Landing alone leaves the field unused (old stitcher doesn't read it). New stitcher in C-5 will populate it at research-complete and consume it in scaffold dispatches.

**What changes**:
- `internal/workflow/recipe_plan.go` — add `SymbolContract` sub-struct matching [atomic-layout.md §4 schema](../03-architecture/atomic-layout.md) (EnvVarsByKind, HTTPRoutes, NATSSubjects, NATSQueues, Hostnames, DTOs, FixRecurrenceRules) [~80 lines]
- `internal/workflow/symbol_contract.go` — new file, `BuildSymbolContract(plan.Research) SymbolContract` with 12 seeded FixRecurrenceRules (nats-separate-creds, s3-uses-api-url, s3-force-path-style, routable-bind, trust-proxy, graceful-shutdown, queue-group, env-self-shadow, gitignore-baseline, env-example-preserved, no-scaffold-test-artifacts, skip-git) [~200 lines]
- `internal/workflow/symbol_contract_test.go` — table-driven tests per CLAUDE.md seed pattern; cases for single-codebase minimal / dual-runtime minimal / showcase-with-worker / showcase-without-worker / empty-managed-services [~250 lines]

**Rough LoC**: +530 lines

**Test layers**: unit

**Breaks alone**: nothing — the field exists but is never populated (Go struct default) and never read. No behavior change.

**Ordering deps**: C-0 (baseline)

**Why before C-5**: the stitcher in C-5 interpolates `{{.SymbolContract | toJSON}}` into scaffold briefs. Landing the schema + derivation first means C-5 is a pure stitcher change, not a stitcher + schema change combined.

---

## C-2 — Add `FactRecord.RouteTo` + ZCP_CONTENT_MANIFEST.json schema extension

**Purpose**: additive field on FactRecord enabling P5 two-way graph. Landing alone: field is optional, existing writer doesn't emit it, existing checks don't read it.

**What changes**:
- `internal/ops/facts_log.go` — extend `FactRecord` with `RouteTo` field typed as `string` with validation against enum `{"content_gotcha", "content_intro", "content_ig", "content_env_comment", "claude_md", "zerops_yaml_comment", "scaffold_preamble", "feature_preamble", "discarded"}` [~20 lines]
- `internal/ops/facts_log_test.go` — cover the new field + validation; ensure existing records without the field still deserialize cleanly (additive contract) [~60 lines]
- `internal/workflow/content_manifest.go` (or equivalent) — extend ZCP_CONTENT_MANIFEST.json schema to require `routed_to` per fact, with the same enum; old manifests without the field continue parsing (zero-value default treated as legacy) [~30 lines]
- `internal/workflow/content_manifest_test.go` — test old-schema + new-schema round-trip; test invalid `routed_to` value rejected [~80 lines]

**Rough LoC**: +190 lines

**Test layers**: unit + tool (tool tests exercise `record_fact` MCP tool payload shape)

**Breaks alone**: nothing — field is optional on both ends.

**Ordering deps**: C-0 (baseline)

**Why before C-8**: C-8 expands `writer_manifest_honesty` to iterate all `(routed_to × surface)` pairs. Needs the field to exist in the schema first.

---

## C-3 — Scaffold `atom_manifest.go` (unused)

**Purpose**: introduce the path-manifest data structure that replaces `recipe_topic_registry.go`'s role under the new architecture. Lands as dead code; C-5 swaps it in.

**What changes**:
- `internal/workflow/atom_manifest.go` — new file. Declares atoms with: `{path, audience, tier-conditional, max-lines}` tuples for all 86 atoms per [atomic-layout.md §1](../03-architecture/atomic-layout.md). Provides `AtomsForPhase(phase) []Atom`, `AtomsForBrief(role, tier) []Atom`, `AtomPath(id) string` helpers [~250 lines]
- `internal/workflow/atom_manifest_test.go` — seed test per CLAUDE.md pattern: every atom path declared exists as a file (verifies after C-4 lands); atom count matches 86; audience enum values clean; tier-conditional atoms correctly tagged [~150 lines — some tests skipped until C-4 lands]

**Rough LoC**: +400 lines

**Test layers**: unit + build-lint (CI grep that the atom path strings match actual filesystem — runs after C-4)

**Breaks alone**: the manifest is not referenced by any production code path. If C-4 never lands, atom_manifest_test.go would fail the path-existence test. Mitigation: atom_manifest_test.go's filesystem assertions run conditional on `testing.Short() == false` with an explicit skip message naming C-4 as the prerequisite.

**Ordering deps**: C-0

**Why now**: keeps data-structure design decoupled from stitcher rewrite. Review surface is bounded to "is the manifest shape correct?" without the stitcher's behavior change blended in.

---

## C-4 — Land 86 atom files under `internal/content/workflows/recipe/`

**Purpose**: physically land the atomic content tree. Does not affect runtime because no code reads from `recipe/` yet (old path reads `recipe.md`; new path would read `recipe/` but C-5 hasn't swapped).

**What changes**:
- `internal/content/workflows/recipe/README.md` — architecture overview (120 lines, non-loaded)
- `internal/content/workflows/recipe/phases/` — 37 files per [atomic-layout.md §1](../03-architecture/atomic-layout.md) (research / provision / generate / deploy / finalize / close)
- `internal/content/workflows/recipe/briefs/` — 26 files across scaffold / feature / writer / code-review
- `internal/content/workflows/recipe/principles/` — 22 files (where-commands-run, file-op-sequencing, tool-use-policy, symbol-naming-contract, todowrite-mirror-only, fact-recording-discipline, platform-principles/*, dev-server-contract, comment-style, visual-style, canonical-output-paths)
- Every atom is ≤300 lines (P6 invariant)
- Every atom carries zero version anchors (P6 invariant; verified by C-13 build lint once that lands)
- Every atom under `briefs/` contains zero dispatcher vocabulary, zero internal check names, zero Go-source paths (P2 invariant; verified by C-13)
- `internal/content/workflows/recipe/` is marked for embedding via `go:embed` directive in the package that loads atoms (set up in C-5)

**Rough LoC**: +6,500 lines of markdown across 86 files. No Go change in this commit.

**Test layers**: unit (smoke test: every file exists at the path `atom_manifest.go` declared in C-3 — completes C-3's skipped assertions); build-lint (preview; full grep guards land in C-13)

**Breaks alone**: nothing — content exists on disk but no code reads it. `go:embed` is not yet declared (embedding happens in C-5's package).

**Ordering deps**: C-3 (atom_manifest.go declares paths these files must exist at)

**Why now**: atoms reviewed independently from stitcher. Review surface per atom is ≤300 lines; reviewer walks [atomic-layout.md §3 block→atom mapping table](../03-architecture/atomic-layout.md) confirming each block's new atoms contain the intended content (minus scar tissue — dispatcher text, version anchors, internal vocabulary, Go paths per [principles.md P2 §Replaces](../03-architecture/principles.md)).

**Review signal**: the step-4 composed briefs (see [`../04-verification/brief-*-composed.md`](../04-verification/)) are the intended stitching outputs; any atom's content is compared against its expected slice of the composed outputs.

---

## C-5 — Rewrite `recipe_guidance.go` stitchers (CUTOVER)

**Purpose**: swap `buildSubStepGuide` (and helpers) from block-lookup-via-recipe.md+topic-registry to atom-concatenation-via-atom_manifest+recipe/ tree. **This is the cutover commit.** Before C-5: agent sees old content. After C-5: agent sees new atomic content.

**What changes**:
- `internal/workflow/recipe_guidance.go` — rewrite `buildSubStepGuide` + eager-composition helpers. New helper signatures per [atomic-layout.md §6](../03-architecture/atomic-layout.md):
  - `buildStepEntry(phase) (string, error)` — concat `phases/<phase>/entry.md` + substep entries + applicable principles atoms
  - `buildSubStepCompletion(phase, substep) (string, error)` — concat substep completion + next-substep entry
  - `buildScaffoldDispatchBrief(contract SymbolContract, codebase string) (string, error)` — concat `briefs/scaffold/mandatory-core.md` + `symbol-contract-consumption.md` (with `{{.SymbolContract | toJSON}}` interpolated) + `framework-task.md` + `<codebase>-codebase-addendum.md` + `pre-ship-assertions.md` + `completion-shape.md` + pointer-include principles + Prior Discoveries block
  - `buildFeatureDispatchBrief(contract, features) (string, error)` — same pattern
  - `buildWriterDispatchBrief(plan, factsPath) (string, error)` — writer atoms
  - `buildCodeReviewDispatchBrief(plan, manifestPath) (string, error)` — code-review atoms
- Embedding: add `//go:embed all:internal/content/workflows/recipe/*` (or package-local variant) so atoms ship with the binary
- `internal/workflow/recipe_guidance_test.go` — replace block-based fixtures with atom-based fixtures; test that (a) every phase's step-entry stitches without error, (b) every (role × tier) brief composes and matches a golden file (the step-4 composed briefs serve as golden files), (c) SymbolContract interpolation produces byte-identical JSON fragments across N scaffold dispatches for the same plan
- `internal/workflow/recipe_brief_facts.go BuildPriorDiscoveriesBlock` — minor extension to include `RouteTo` filter for writer/code-review lanes (per [migration-proposal.md §1.2](migration-proposal.md))
- `internal/workflow/recipe_substeps.go` — tier-branching helpers updated to match atom_manifest tier-conditional flags
- Plan-research step hook: populate `plan.Research.SymbolContract = BuildSymbolContract(plan.Research)` at `complete step=research` before dispatching provision step-entry [per data-flow-showcase.md §1 step (7)]

**Rough LoC**: ~-400 LoC (block-composition deletion) + ~+500 LoC (atom-composition rewrite) + ~+400 LoC test = ~+500 LoC net

**Test layers**: unit + tool + integration (existing integration harness exercises full phase transitions; tests must assert composed-brief content against step-4 golden files)

**Breaks alone**: if shipped alone after C-4 (atoms landed), agent sees new content with all new invariants — but some checks (C-6 new, C-8 expanded, C-10 shrunken payload) haven't landed. Result: agent follows new content, but gate still emits verbose failure payloads and lacks the 5 new checks. Not catastrophic: old gate shape against new content is a **mildly lossy** state, not broken. Still, the v35 thesis requires C-5 + C-6 + C-7 + C-8 + C-10 all landed for convergence measurement to be valid.

**Ordering deps**: C-1 (SymbolContract), C-3 (atom_manifest), C-4 (atom files)

**Why here**: cutover is unavoidable for stitcher body. Per [migration-proposal.md §2.1](migration-proposal.md), `buildSubStepGuide` is one function; cohabiting both bodies via feature flag is the parallel-run candidate that was rejected. Cleanroom's single-commit cutover is the direct path.

**Risk mitigation**: C-14 lands a `zcp dry-run recipe` harness that exercises `buildSubStepGuide` across every (phase × tier) producing composed output, which can be diffed against step-4 golden files in CI — before the cutover ships to v35.

---

## C-6 — Add 5 new architecture-level checks

**Purpose**: introduce the 5 new checks per [check-rewrite.md §16](../03-architecture/check-rewrite.md): `symbol_contract_env_var_consistency` (P3), `visual_style_ascii_only` (P8), `canonical_output_tree_only` (P8), `manifest_route_to_populated` (P5), `no_version_anchors_in_published_content` (P6).

**What changes**:
- `internal/tools/workflow_checks_symbol_contract.go` — new file with the predicate function `CheckSymbolContractEnvVarConsistency(mountRoot) []StepCheck`; diffs env-var tokens across `{host}/src/**/*.{ts,js,php,go}` + `{host}/.env.example` + `{host}/zerops.yaml` against the SymbolContract's `EnvVarsByKind` [~150 lines]
- `internal/tools/workflow_checks_visual_style.go` — new file; `CheckVisualStyleAsciiOnly(mountRoot) []StepCheck`; greps `*/zerops.yaml` for Unicode Box-Drawing codepoints `[\x{2500}-\x{257F}]` [~50 lines]
- `internal/tools/workflow_checks_canonical_output_tree.go` — new file; `CheckCanonicalOutputTreeOnly(mountRoot) []StepCheck`; `find /var/www -maxdepth 2 -type d -name 'recipe-*'` returns empty [~60 lines]
- `internal/tools/workflow_checks_manifest_route_to.go` — new file; `CheckManifestRouteToPopulated(manifestPath) []StepCheck`; every `.facts[].routed_to` non-empty [~50 lines]
- `internal/tools/workflow_checks_no_version_anchors_in_published.go` — new file; `CheckNoVersionAnchorsInPublishedContent(mountRoot) []StepCheck`; greps `{host}/README.md`, `{host}/CLAUDE.md`, `environments/*/README.md` for `v\d+(\.\d+)*` [~70 lines]
- Per-check unit test files per CLAUDE.md seed pattern [~600 lines total]
- Register in `internal/workflow/recipe_substeps.go` + checker dispatch at the substeps per [check-rewrite.md §16 "Added to" column](../03-architecture/check-rewrite.md): symbol-contract + manifest-route-to at deploy.readmes; visual-style at generate-complete; canonical-output-tree at close-entry; version-anchors at finalize-complete.

**Rough LoC**: +980 lines

**Test layers**: unit (predicate function) + tool (checker-dispatch registration) + integration (end-to-end phase-complete triggers the new checks)

**Breaks alone**: new gates fire. If v35 candidate mount has visible defects (e.g. version anchor in a published README), the new gate catches it — which is the intended behavior. No regression against prior runs because the target is a clean v35 deliverable.

**Ordering deps**: C-1 (SymbolContract field for symbol-contract-env-consistency), C-2 (RouteTo field for manifest-route-to-populated)

---

## C-7 — Rewrite 16 checks to runnable form + add `zcp check <name>` CLI shim surface

**Purpose**: per [check-rewrite.md §17 + §18](../03-architecture/check-rewrite.md), refactor 16 existing checks' predicate bodies into reusable `ops/checks/*.go` functions and add paired `zcp check <name>` CLI subcommands. Design invariant: **gate and shim call the same Go function** (impossible to diverge).

**What changes**:
- `internal/ops/checks/*.go` — new package. One file per rewritten check, each exporting `func Check<Name>(ctx, args) []StepCheck`. 16 files total per [check-rewrite.md §18 subcommand list](../03-architecture/check-rewrite.md): env-refs, run-start-build-contract, env-self-shadow, ig-code-adjustment, ig-per-item-code, comment-specificity, yml-schema, kb-authenticity, worker-queue-group-gotcha, worker-shutdown-gotcha, manifest-honesty (expanded version lands in C-8), manifest-completeness, comment-depth, factual-claims, cross-readme-dedup, symbol-contract-env-consistency (re-expose C-6's predicate) [~1,600 LoC total — bodies mostly moved from `workflow_checks_*.go`, not rewritten from scratch]
- `cmd/zcp/check/` — new subcommand tree. `cmd/zcp/check/check.go` + 16 files `cmd/zcp/check/<name>.go`, each wiring CLI flags to `ops/checks.Check<Name>` [~600 LoC]
- `internal/tools/workflow_checks_*.go` — existing checks updated to call into `ops/checks/` for the 16 rewritten predicates; rest unchanged [~200 LoC updated]
- Per-check unit tests in `internal/ops/checks/*_test.go` [~800 LoC]
- CLI-surface integration test: `cmd/zcp/check/check_integration_test.go` exercises each subcommand against a fixture mount [~300 LoC]

**Rough LoC**: ~+3,500 LoC (mostly relocated predicate bodies) including tests

**Test layers**: unit + tool + integration

**Breaks alone**: gates keep working (predicates shared); CLI shim works standalone. Authors can invoke shims before attesting — the author-runnable axis lands here. Convergence improvement starts accruing even if subsequent commits slip (though C-10 payload shrink is part of the convergence thesis).

**Ordering deps**: C-1 (SymbolContract for symbol-contract shim), C-2 (RouteTo for manifest-* shims), C-6 (new checks to register as shims)

---

## C-7.5 — Add editorial-review role (sub-agent + substep + dispatch-runnable checks)

**Purpose**: add the editorial-review sub-agent role prescribed by [`docs/spec-content-surfaces.md`](../../spec-content-surfaces.md) line 317-319 and scoped in the 2026-04-20 refinement. Closes the classification-error-at-source class (writer self-classification errors escape all prior gates because the manifest faithfully reflects the wrong classification) + defense-in-depth on v28 wrong-surface + v28 folk-doctrine + v28 cross-surface-duplication + v34 self-referential + v34 manifest-content-inconsistency classes.

**What changes**:

- `internal/content/workflows/recipe/briefs/editorial-review/` — new directory with 10 atom files per [`atomic-layout.md §1`](../03-architecture/atomic-layout.md): `mandatory-core.md` + `porter-premise.md` + `surface-walk-task.md` + `single-question-tests.md` + `classification-reclassify.md` + `citation-audit.md` + `counter-example-reference.md` + `cross-surface-ledger.md` + `reporting-taxonomy.md` + `completion-shape.md`. Every atom ≤300 lines (P6), audience declared `editorial-review-sub` (P2), zero version anchors, zero dispatcher vocabulary, zero internal check-names, zero Go-source paths. Content sourced from `docs/spec-content-surfaces.md` per-surface contracts + classification taxonomy + counter-examples + citation map.
- `internal/workflow/atom_manifest.go` — register the 10 new atoms with their paths + audience + tier-conditional flags (none tier-conditional — editorial-review applies to both tiers). Updates atom count from 86 to 96 per C-3 manifest.
- `internal/workflow/recipe_guidance.go` — add `buildEditorialReviewDispatchBrief(plan, factsLogPath, manifestPath) (string, error)` stitcher function. Concatenates atoms per [`atomic-layout.md §6`](../03-architecture/atomic-layout.md). Interpolates `{factsLogPath, manifestPath}` as pointer strings (not pre-read content). **Does NOT** include Prior Discoveries block (per refinement §10 open-question #6 — porter-premise requires fresh-reader stance).
- `internal/workflow/recipe_substeps.go` — register `close.editorial-review` substep:
  - showcase: **gated** substep (added before `close.code-review`; `close.code-review` becomes second gated substep, `close.close-browser-walk` third)
  - minimal: **ungated-discretionary** default-on (via `closeSubSteps` returning minimal's discretionary list including editorial-review + code-review, neither gated per minimal's ungated close pattern)
- `internal/tools/workflow_checks_editorial_review.go` — new file registering 7 editorial-review-originated checks per [`check-rewrite.md §16a`](../03-architecture/check-rewrite.md): `editorial_review_dispatched`, `editorial_review_no_wrong_surface_crit`, `editorial_review_reclassification_delta`, `editorial_review_no_fabricated_mechanism`, `editorial_review_citation_coverage`, `editorial_review_cross_surface_duplication`, `editorial_review_wrong_count`. Predicate functions read `Sub[editorial-review].return.*` fields from the substep-complete payload. Unlike §16 new checks, these are **dispatch-runnable** (no shell-shim equivalent — review IS the runner).
- `internal/tools/workflow_checks_editorial_review_test.go` — table-driven tests per CLAUDE.md seed pattern.
- Tier branching: `buildEditorialReviewDispatchBrief` tier-branches in `surface-walk-task.md` (minimal walks 1 codebase × 4 env tiers, no worker codebase; showcase walks 3 codebases × 6 env tiers + worker); all other atoms tier-invariant.

**Rough LoC**: +~2,800 LoC (10 atom .md files at ~80 lines each = ~800 md; stitcher + substep registration ~200 Go; 7 checks + tests ~600 Go; tier-branch handling in surface-walk ~200 Go/md; test fixtures ~1,000).

**Test layers**: unit (stitcher + check predicates) + tool (close.editorial-review registration + substep dispatch wiring) + integration (end-to-end editorial-review dispatch → return → check dispatch flow against a golden deliverable fixture)

**Breaks alone**: editorial-review fires at close. If shipped before C-8 (`writer_manifest_honesty` P5 expansion), editorial-review may catch manifest-consistency issues that would have been caught earlier at `deploy.readmes` under the expanded check. Not a regression — two layers of enforcement, earlier-catch-preferred. Editorial-review's dispatch-runnable checks complement the shell-runnable manifest-honesty check; they don't duplicate.

**Ordering deps**: C-1 (SymbolContract for tier-branch awareness), C-2 (RouteTo for classification-reclassify cross-check), C-3 (atom_manifest for atom registration), C-4 (atoms land — editorial-review atoms are part of the 96 landed in C-4 per updated count), C-5 (stitcher exists so `buildEditorialReviewDispatchBrief` can be added alongside), C-6 (new check infra), C-7 (shim surface conventions — though editorial-review-originated checks don't ship shims, reusing the check-dispatch framework).

**Why here (between C-7 and C-8)**:
- After C-5..C-7: stitcher + check infrastructure exist.
- Before C-8: editorial-review's reclassification catches writer-classification errors that P5's expanded honesty can't (P5 catches "manifest-to-content mismatch"; editorial reclassification catches "classification wrong at source"). Shipping editorial-review first means C-8 is refining a check whose upstream is already more robust.
- Before C-10: editorial-review-originated checks populate `StepCheck` rows. C-10's payload shape change applies uniformly across all checks including the new editorial-review set.

**Pre-ship gate**: every editorial-review atom passes [`principles.md §P7`](../03-architecture/principles.md) cold-read. The composed brief (per `buildEditorialReviewDispatchBrief`) is reviewable in under 3 minutes by a fresh reader. Step-4 verification artifacts for `(editorial-review × showcase)` + `(editorial-review × minimal)` ship in `docs/zcprecipator2/04-verification/` alongside this commit's land.

**Risk mitigation**: editorial-review is the one role with no current-system predecessor — no v34 captured dispatch to byte-diff against. Mitigation:
- Step-4 cold-read simulation + defect-class coverage audit (per P7).
- C-14 dry-run harness exercises `buildEditorialReviewDispatchBrief` across (tier × tier-branch) matrix.
- First v35 run's editorial-review output is the first empirical signal; v35.5 minimal confirms Path B dispatch shape for minimal tier.

---

## C-8 — Expand `writer_manifest_honesty` to all routing dimensions (P5)

**Purpose**: per [check-rewrite.md §12](../03-architecture/check-rewrite.md) + [principles.md P5](../03-architecture/principles.md) — extend the single-dimension `(discarded, published_gotcha)` check to all 6 `(routed_to × surface)` pairs. This is the v34 DB_PASS closure.

**What changes**:
- `internal/tools/workflow_checks_content_manifest.go checkManifestHonesty` — rewrite to iterate all pairs; emit one `StepCheck` row per pair with distinct names (`writer_manifest_honesty_discarded_as_gotcha`, `writer_manifest_honesty_claude_md_as_gotcha`, `writer_manifest_honesty_integration_guide_as_gotcha`, `writer_manifest_honesty_zerops_yaml_comment_as_gotcha`, `writer_manifest_honesty_env_comment_as_gotcha`, `writer_manifest_honesty_any_as_intro`) [~100 LoC delta]
- `internal/ops/checks/manifest_honesty.go` — the `zcp check manifest-honesty` shim body; same predicate body [~80 LoC; already scaffolded in C-7, populated here]
- `internal/tools/workflow_checks_content_manifest_test.go` — test cases per routing dimension; the v34 DB_PASS scenario as a regression fixture (fact with `routed_to=claude_md` + README knowledge-base containing fact title tokens → expect fail) [~250 LoC]
- `briefs/code-review/manifest-consumption.md` — atom updated to instruct code-review sub-agent to verify manifest consistency along all 6 routing dimensions (already landed in C-4; confirm byte-for-byte alignment with new check names here)

**Rough LoC**: +350 LoC (mostly tests)

**Test layers**: unit (predicate) + tool (gate dispatch) + integration (full writer-dispatch → manifest-emit → check-dispatch flow)

**Breaks alone**: stricter gate at `deploy.readmes` complete. If v35 candidate's writer emits a manifest that trips any of the new dimensions, the gate fails with a per-dimension check name. Expected: v35 writer atom content (C-4) already teaches routing honesty along all dimensions; first-pass writer output should pass.

**Ordering deps**: C-2 (`RouteTo` field), C-7 (shim surface infrastructure)

---

## C-9 — Delete `knowledge_base_exceeds_predecessor`

**Purpose**: per [check-rewrite.md §15](../03-architecture/check-rewrite.md), drop the one check marked for definite deletion. Informational-only since v8.78; zero gate value; upstreamed by `knowledge_base_gotchas` + `knowledge_base_authenticity`.

**What changes**:
- `internal/tools/workflow_checks_predecessor_floor.go` — remove `CheckKnowledgeBaseExceedsPredecessor` function + its registration in the substep-dispatch; keep `CheckKnowledgeBaseAuthenticity` (rewrite-to-runnable landed in C-7)
- `internal/tools/workflow_checks_predecessor_floor_test.go` — remove tests for the deleted predicate
- Brief / atom content: confirm no atom under `briefs/writer/*` references `knowledge_base_exceeds_predecessor` by name (P2 invariant) — should already hold from C-4

**Rough LoC**: -70 LoC

**Test layers**: unit + tool

**Breaks alone**: one less informational gate fires. Zero regression risk — check has been informational since v8.78.

**Ordering deps**: C-7 (authenticity-rewrite landed as the upstream replacement)

---

## C-10 — Shrink `StepCheck` failure payload to `{name, detail, preAttestCmd, expectedExit}`

**Purpose**: per [data-flow-showcase.md §9](../03-architecture/data-flow-showcase.md) + [principles.md P1 §Replaces](../03-architecture/principles.md) — drop the v8.96 Theme A `ReadSurface/Required/Actual/CoupledWith/HowToFix` fields and v8.104 Fix E `PerturbsChecks` field from the agent-facing payload. **Breaking shape change.**

**What changes**:
- `internal/workflow/step_check.go` (or equivalent) — `StepCheckResult` / `CheckFailurePayload` struct shape: remove the 6 verbose fields; add `PreAttestCmd string` + `ExpectedExit int`
- All 78 checks (including rewritten-in-C-7 and new-in-C-6) updated to emit `PreAttestCmd` per [check-rewrite.md runnable-form columns](../03-architecture/check-rewrite.md) instead of verbose fields [~300 LoC across 12+ workflow_checks_*.go files; most changes are "construct one command string instead of 5-6 advisory fields"]
- `internal/tools/workflow.go` failure-payload emission — drop emission of the 6 fields
- `internal/workflow/stamp_coupling.go` — delete (v8.97 Fix 4 surface-derived coupling; P1 supersedes per [registry row 12.4](../05-regression/defect-class-registry.md))
- `internal/tools/bootstrap_checks.go` — remove `StampCoupling` invocation
- Tests: across the 12 `workflow_checks_*_test.go` files, assertions against `ReadSurface / HowToFix / CoupledWith / PerturbsChecks` are replaced by assertions against `PreAttestCmd` [~400 LoC updated]
- **Debug log retention**: the verbose fields are NOT deleted from the server-side structured log; they're dropped only from the agent-facing payload. Add `debug.LogCheckFailureDetail(ctx, check, readSurface, required, actual, ...)` for server-side human inspection (gated by `--debug` flag already present) [~50 LoC]

**Rough LoC**: -100 LoC net (deletions roughly balance PreAttestCmd additions); ~+400 LoC test updates

**Test layers**: unit + tool + integration (integration tests assert the agent-facing payload shape matches the shrunken contract; debug-log tests assert the rich detail is preserved server-side)

**Breaks alone**: agent loses the verbose fields. Before C-5 cutover lands, this would be a regression (old content-tree teaches the agent to consume verbose fields). Combined with C-5, the new content-tree teaches the agent the new payload shape.

**Ordering deps**: C-5 (cutover), C-6, C-7, C-8, C-9 (all check changes landed; payload shape now flipped)

**Why late**: the payload shape change is the most breaking axis. Sequencing it after C-5 + all check commits lets each commit live at an internally-consistent payload-shape snapshot. When C-10 lands, the snapshot flips atomically.

---

## C-11 — Empty `NextSteps[]` at close completion

**Purpose**: per [principles.md P4 §Replaces](../03-architecture/principles.md) + [data-flow-showcase.md §6b](../03-architecture/data-flow-showcase.md) — close-step response carries `NextSteps=[]`. Export and publish are user-request-only (v8.103 held; this commit makes the empty default explicit).

**What changes**:
- `internal/workflow/recipe_substeps.go` — the `complete step=close` response builder sets `NextSteps = []` unconditionally
- `internal/tools/workflow_close_next_steps_test.go` — update the test from C-0 (which captured old behavior) to assert the new empty-by-default behavior
- `internal/workflow/recipe_workflow_integration_test.go` — confirm no follow-up `zcp sync recipe export` / `publish` invocations fire autonomously after close complete

**Rough LoC**: -30 LoC + 100 LoC test

**Test layers**: tool + integration

**Breaks alone**: post-close autonomous tool invocations stop firing. Matches v8.103 export-on-request invariant. Not a regression; makes the invariant structural instead of content-only.

**Ordering deps**: C-0 (baseline test)

---

## C-12 — Land `docs/zcprecipator2/DISPATCH.md`

**Purpose**: human-facing dispatch composition guide. Per [atomic-layout.md §2](../03-architecture/atomic-layout.md) + [principles.md P2 §Enforcement](../03-architecture/principles.md) — dispatcher instructions live in a document that is **never transmitted** to sub-agents.

**What changes**:
- `docs/zcprecipator2/DISPATCH.md` — new file. Covers: how to compose a scaffold dispatch from `briefs/scaffold/*`; how to interpolate `SymbolContract`; how to handle single-codebase vs multi-codebase branching; how to assemble the feature / writer / code-review dispatches; why version anchors, internal check names, Go-source paths, and dispatcher vocabulary are forbidden in transmitted briefs (with pointers to the build-lint rules that enforce)
- No Go code change

**Rough LoC**: +400 lines (docs only)

**Test layers**: none runtime; CI docs-lint if applicable

**Breaks alone**: nothing — documentation-only

**Ordering deps**: C-4 (atoms exist to reference) + C-5 (stitcher exists to reference)

---

## C-13 — Build-time lints on the atomic content tree

**Purpose**: enforce P2 / P6 / P8 invariants mechanically. Per [principles.md P2 §Enforcement](../03-architecture/principles.md) + [P6 §Enforcement](../03-architecture/principles.md) + [P8 §Enforcement](../03-architecture/principles.md) + [calibration-bars-v35.md §9 B-1..B-8](../05-regression/calibration-bars-v35.md).

**What changes**:
- `tools/lint/recipe_atom_lint.go` (or shell script under `tools/lint/`) — new lint that runs as part of `make lint-local`:
  - **B-1**: `grep -rE 'v[0-9]+(\.[0-9]+)*|v8\.[0-9]+' internal/content/workflows/recipe/` returns empty
  - **B-2**: `grep -riE 'compress|verbatim|include as-is|main agent|dispatcher' internal/content/workflows/recipe/briefs/` returns empty
  - **B-3**: `grep -riE 'writer_manifest_|_env_self_shadow|_content_reality|_causal_anchor' internal/content/workflows/recipe/briefs/` returns empty
  - **B-4**: `grep -rE 'internal/[^ ]*\.go' internal/content/workflows/recipe/briefs/` returns empty
  - **B-5**: `find internal/content/workflows/recipe/ -name '*.md' -exec wc -l {} +` — no file exceeds 300 lines
  - **B-7**: orphan-prohibition lint — any atom containing "do not", "avoid", "never", "MUST NOT" must also contain a positive-form statement in the same atom (heuristic: those tokens must co-appear with explicit positive-form phrases in the 10 lines surrounding)
  - **H-4**: step-entry atoms `phases/*/entry.md` — forbidden phrasing "your tasks for this phase are" absent (positive P4 form required)
- `Makefile` — register `lint-recipe-atoms` in `lint-local` target
- CI configuration — run the lint on every PR

**Rough LoC**: +250 LoC

**Test layers**: build-lint (this IS the build lint)

**Breaks alone**: CI gates on atom compliance. Atoms from C-4 were authored to be compliant — the lint is a ratchet that prevents regression on future atom edits.

**Ordering deps**: C-4 (atoms exist)

---

## C-14 — v35 dry-run infrastructure + calibration-bar measurement scripts

**Purpose**: enable pre-ship + post-ship measurement against the 97 bars in [calibration-bars-v35.md](../05-regression/calibration-bars-v35.md).

**What changes**:
- `cmd/zcp/dry_run_recipe.go` — new subcommand `zcp dry-run recipe --tier=<showcase|minimal> --dual-runtime=<true|false> --against=<fixture-plan.json>`. Exercises `buildSubStepGuide` across every (phase × tier) producing composed output; diffs against step-4 golden files (see `docs/zcprecipator2/04-verification/brief-*-composed.md`); reports atom-by-atom delta [~300 LoC]
- `scripts/measure_calibration_bars.sh` — shell driver that, given a v35 session log path + exported deliverable tree path, evaluates every bar in `calibration-bars-v35.md §1–§11` and emits `reports/v35-measurement-<timestamp>.md` crossing each bar with PASS/FAIL + evidence [~400 LoC of shell / Python]
- `scripts/extract_calibration_evidence.py` — helper for bars needing session-log parse (C-1 deploy rounds, C-2 finalize rounds, C-6 out-of-order substep-completes, C-11 TodoWrite full-rewrite detection, etc.); reuses logic from [docs/zcprecipator2/scripts/extract_flow.py](../scripts/extract_flow.py) [~300 LoC]
- `scripts/measure_calibration_bars_test.sh` — validates the driver against v34 session logs (known baselines) + a synthetic all-pass fixture [~150 LoC]

**Rough LoC**: +1,150 LoC

**Test layers**: integration (scripts run against v34 fixtures to assert known results)

**Breaks alone**: nothing — adds measurement surface

**Ordering deps**: C-5 (stitcher exists so dry-run can exercise it), C-6 + C-7 + C-8 + C-9 + C-10 (check surface finalized so measurement is against target state)

**Pre-ship gate**: before v35 is commissioned, `zcp dry-run recipe --tier=showcase` passes against step-4 golden files; `make lint-local` passes; `go test ./... -count=1` passes; `scripts/measure_calibration_bars.sh` against v34 fixtures produces known results.

---

## C-15 — Delete `recipe.md` + `recipe_topic_registry.go`

**Purpose**: remove the old monolith + old topic registry now that the cutover (C-5) has been verified via dry-run (C-14). Final cleanup.

**What changes**:
- `internal/content/workflows/recipe.md` — delete (3,438 lines removed)
- `internal/workflow/recipe_topic_registry.go` — delete
- `internal/workflow/recipe_topic_registry_test.go` — delete
- Remove any remaining references in Go code to `recipe.md` or `recipe_topic_registry` (expected to be zero after C-5 cutover; any found indicate incomplete C-5)

**Rough LoC**: -3,700 LoC (mostly markdown) plus ~-300 LoC (registry + tests)

**Test layers**: build-lint (no remaining reference), unit + tool + integration (full regression after removal — every test must still pass)

**Breaks alone**: compilation fails if any Go code still references the old symbols. C-5 must have cut over cleanly.

**Ordering deps**: C-5 through C-14 all landed; `make lint-local` + `go test ./...` + `zcp dry-run recipe` all passing.

**Why last**: conservative — the old monolith stays around through C-14 so any cutover bug can be root-caused by diffing old vs new emission without needing to revert C-5. C-15 removes the old path only when the new path is empirically validated via dry-run + every test layer.

---

## Summary

| Commit | Δ LoC | Test layers | Revertable | Ships alone = |
|---|---:|---|---|---|
| C-0 | +400 | unit + tool | trivially | tests expanded |
| C-1 | +530 | unit | trivially | unused plan field |
| C-2 | +190 | unit + tool | trivially | unused manifest field |
| C-3 | +400 | unit + build-lint | trivially | dead code |
| C-4 | +7,400 md | (none runtime) | trivially | dead content (includes 10 editorial-review atoms = ~800 md) |
| C-5 | +500 net | unit + tool + integration | yes (reverts stitcher) | **cutover** |
| C-6 | +980 | unit + tool + integration | yes | 5 new gates fire |
| C-7 | +3,500 | unit + tool + integration | yes | shim CLI surface added |
| **C-7.5** | **+2,800** | **unit + tool + integration** | **yes** | **editorial-review role active at close (refinement 2026-04-20)** |
| C-8 | +350 | unit + tool + integration | yes | stricter gate at deploy.readmes |
| C-9 | -70 | unit + tool | yes | one less informational gate |
| C-10 | -100 net + 400 test | unit + tool + integration | **breaking shape** | agent payload shape flipped |
| C-11 | -30 + 100 test | tool + integration | yes | close responses stop suggesting autonomous export |
| C-12 | +400 docs | (none) | trivially | docs-only (DISPATCH.md covers editorial-review composition) |
| C-13 | +250 | build-lint | yes | CI gates atoms (editorial-review atoms included in lint surface) |
| C-14 | +1,150 | integration | yes | measurement surface added (dry-run exercises editorial-review stitcher) |
| C-15 | -4,000 | all layers | **finalization** | old monolith + registry gone |

Total rollout: **~+14,200 net LoC** (markdown-dominated) across **16 commits**, each individually revertable.

**End-state cross-check**:
- Every [check-rewrite.md §17](../03-architecture/check-rewrite.md) disposition accounted for: C-6 (+5 new), C-7 (16 rewritten), C-7.5 (+7 editorial-review-originated), C-8 (manifest honesty expansion), C-9 (-1 deletion). Total 56 keep + 16 rewrite + 1 delete + 5 new + 7 editorial-review = **85** ✓
- Every [atomic-layout.md §1](../03-architecture/atomic-layout.md) atom accounted for: C-4 lands all **96** (86 original + 10 editorial-review). ✓
- Every [principles.md §10](../03-architecture/principles.md) enforcement-layer mapping covered:
  - P1 runnable pre-attest → C-6 + C-7 + C-10 (`PreAttestCmd` on wire); **editorial-review in C-7.5 extends P1 semantics: reviewer IS the runnable form for editorial predicates that shell shims cannot express (semantic reader-intent + cross-framework-docs surprise)**
  - P2 leaf briefs + grep guard → C-4 + C-12 + C-13; editorial-review atoms (C-4 + C-7.5) conform to P2
  - P3 SymbolContract → C-1 + C-5 (interpolation) + C-6 (symbol-contract-env-consistency check)
  - P4 server state = plan → C-4 (positive phrasing atoms) + C-11 (empty NextSteps); close.editorial-review is a new substep enforcing P4
  - P5 two-way graph → C-2 (RouteTo) + C-6 (manifest-route-to-populated) + C-8 (expanded honesty); **editorial-review in C-7.5 adds THIRD layer: independent reclassification catches classification-error-at-source that P5's manifest-honesty can't (P5 catches mismatch between manifest and content; editorial catches classification wrong at source)**
  - P6 atomic + 300-line cap + no version anchors → C-4 + C-13; editorial-review atoms ≤300 lines, no anchors
  - P7 cold-read + defect-coverage gate → step-4 deliverables (pre-rollout) + C-14 (measurement post-rollout); **editorial-review in C-7.5 institutionalizes P7 at runtime (not just pre-merge) — every v35+ run has a sub-agent that applies cold-read tests to the deliverable**
  - P8 positive allow-list → C-4 (atom content) + C-13 (orphan-prohibition lint) + C-6 (canonical-output-tree-only + visual-style-ascii-only); editorial-review `reporting-taxonomy.md` declares positive form (CRIT/WRONG/STYLE semantics + inline-fix policy) not forbidden-verdict enumeration

All 8 principles covered. All 68 defect-class-registry rows covered (69 rows after refinement adds classification-error-at-source). All 97 → 102 calibration bars measurable (5 new editorial-review bars).

---

## Parallelization opportunities

Within the sequence, some commits can be authored in parallel (same author, separate branches that merge in order; OR separate authors):

- C-1, C-2, C-3, C-4 can be authored in parallel after C-0
- C-6, C-8 can be authored in parallel after C-5 (independent checks)
- C-7 is the largest commit; could be split into 4 sub-commits (env-refs + run-start-build + env-self-shadow + yml-schema | ig-*-code + comment-specificity | kb-authenticity + worker-* | manifest-* + comment-depth + factual-claims + cross-readme-dedup + symbol-contract-env-consistency)
- C-7.5 can be authored in parallel with C-8 after C-7 (both add close-phase enforcement layers; editorial-review and P5 expansion operate at different phases — close vs deploy.readmes — so independent)
- C-11, C-12, C-13 can be authored in parallel after C-10

Sequencing at merge must preserve the order above to keep each merged state internally consistent.
