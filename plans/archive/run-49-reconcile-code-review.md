# Reconcile branch code review — `reconcile-stranded-work-2026-05-25`

> **TL;DR**: Branch `reconcile-stranded-work-2026-05-25` cleanly combines
> v9.99.0 (kb-surface-recalibration) + v9.98.0 (corpus-recalibration-
> 2026-05-20) on top of current `main`. Two real content conflicts, both
> resolved in favor of the underlying intent (v9.99.0's spec rewrite
> stands; v9.98.0's corrected env-model wins where it specifically
> updates wrong env text). One legitimate cap bump (refinement-brief
> 50→52 KB). **Full test suite green** (`go test ./...` clean across
> 20 packages). 96 files changed, 2 added, ~1.9 KLOC net.
> **Safe to land on main** — but the Surface 5 spec rewrite from
> v9.99.0 is a deliberate calibration shift, not a fix, so the merge to
> main should be a conscious sign-off rather than a routine fast-forward.

---

## Branch composition

```
a7e0c386 reconcile: bump refinement-brief soft cap 50→52 KB
ef94252e merge: v9.98.0 corpus-recalibration-2026-05-20
157b7de8 merge: v9.99.0 kb-surface-recalibration
568ae962 ← main HEAD (untouched)
```

| | Source | Commits | Files touched | Net LoC |
|---|---|---:|---:|---:|
| v9.99.0 merge | `kb-surface-recalibration` | 12 (your authorship, 2026-05-15→22) | 21 | +932 / -300 |
| v9.98.0 merge | `corpus-recalibration-2026-05-20` | 7 (your authorship, 2026-05-20) | 30 | +988 / -452 |
| Reconcile fix-up | this session | 1 (cap bump) | 1 | +9 / -2 |

### Test state

- `go build ./...` ✓ (clean)
- `go test ./... -short` ✓ across all 20 packages (recipe, tools, workflow, ops, ops/bundle, ops/checks, ops/cicd, content, init/adapters, knowledge, platform, preprocess, runtime, schema, server, service, sync, topology, update, lint, lint/atom_template_vars)
- One transient test failure before cap bump (refinement brief 51,426 bytes > 50,201 cap) — addressed in commit `a7e0c386`

---

## v9.99.0 merge — review

### Surface 5 spec rewrite (cc8c0305, `docs/spec-content-surfaces.md` §Surface 5)

**Verdict: SOUND — but a deliberate calibration shift, not a fix.** The argument is defensible: calibrating Surface 5's contract against `laravel-showcase` (which is engine output) creates a positive-feedback loop where future engines optimize against their own past outputs. Recalibrating against `laravel-jetstream` (human-authored anchor) breaks that loop.

The new contract:
- Plural Reader: evaluation audience + search-arrival audience
- Test framed as "constraint or adaptation cost" (decision-affecting), not "symptom recognition" (debugging)
- Two valid shapes: (1) forward-looking H3 operational sections — jetstream-shape — (2) symptom-first `### Gotchas` bullets — engine-convention — opted into explicitly via `### Gotchas` header OR implicitly via bold-stem bullets
- ItemCap=8 retired ("salience, not count")
- Empty KB body permitted as a positive signal

**Risk surface**: Existing recipes shipped with the old Surface 5 contract (symptom-first mandate, ≤8 cap) will not retroactively rebreak — the cap retirement is purely permissive. But the new shape (1) (forward-looking H3 operational) may take engine runs to fully manifest at the recipe surface; sub-agents have priors trained on the old symptom-first shape.

**Spec-prose quality**: Read fluently. The "Two valid shapes — pick by content, not by template" framing is exactly what stops sub-agents from defaulting to one shape per template. Counter-examples (Self-inflicted-reversible + Pure framework/library facts) are concrete and traceable to run-48 audit cases.

### New gate: `kb-self-inflicted-reversible` (9ff816ec + 4a44a291 + 4f07b948)

**Verdict: WELL-SCOPED.** The gate's design is unusually careful:

- **Join key**: `(codebase_hostname, surface_kind, directive_path)` — the right shape to discriminate "porter undoes recipe directive" from "porter hits intersection symptom"
- **Trigger detection**: case-insensitive regex per known run-48 pattern, scoped per codebase KB fragment body
- **Discriminator**: every pattern carries a `HasDirective` scanner that reads the recipe's actual artifacts; pattern fires ONLY when the matching directive ships
- **False-positive minimization explicit in design**: codex code-review F4 fix (4f07b948) tightens `stripAllYAMLComments` so a yaml that mentions a directive only in a comment does NOT count as "shipped" — this is the discipline an unbiased gate needs

**v1 pattern coverage** (5 patterns covering the run-48 audit's named cases):
1. `env-file-in-deployed-tree` ← `ignoreEnvFile: true` ships
2. `custom-response-headers-undefined-from-spa` ← `exposedHeaders` ships
3. `start-directive-on-base-static` ← `base: static` ships
4. `execonce-key-collision` ← `initCommands + zsc execOnce` ships
5. `ioredis-auth-against-unauth-valkey` ← no cache password alias ships

**Test surface**: 4 pin tests covering 5 REFUSE + 2 POSITIVE + 1 EDGE empty-body case + EmptyPlan + NoDirectiveShipped boundary + registration. Boundary cases (empty-body, directive-only-in-yaml-comment) are explicitly named.

**Concerns**: None significant. The gate is additive and blocking; if a future false-positive surfaces, the pattern list + per-pattern HasDirective callback are extensible. The 4a44a291 cleanup (drop unused `runStartRE`) is correct — the `base: static` discriminator alone fires the static-runtime case.

### Surface 5 validator/assembler boundary changes (75db7b68, f110e3da)

**Verdict: CLEAN.** Three changes derived directly from the spec:

1. `validators_codebase.go::validateCodebaseKB`:
   - `codebase-kb-marker-missing` fires only on true marker absence (empty body between present markers is legitimate)
   - `kb-missing-bold-symptom` gated on `isSymptomFirstShape` so forward-looking H3 bodies pass without per-bullet enforcement
   - `codebase-kb-too-many-bullets` removed (cap retired)

2. `slot_shape.go::checkCodebaseKBAll`:
   - Symptom-first stem regex gated on `isSymptomFirstShape`
   - 8-bullet cap removed

3. `assemble.go::substituteFragmentMarkers` + `isKBFragmentID`:
   - Missing-OR-blank `codebase/<host>/knowledge-base` is no longer accumulated as missing; assembler emits well-formed empty markers
   - Scope narrowed by `isKBFragmentID` to ONLY that id pattern; every other surface still fails loudly on missing

**Test renames track behaviour**: `TestCodebaseKB_BulletCap` → `TestCodebaseKB_NoBulletCap`; `TestCheckSlotShape_KB_RefusesNonTopicBullet` split into shape-(1)-passes / shape-(2)-opt-in-refuses; `TestCheckSlotShape_KB_RefusesOverEightBullets` → `TestCheckSlotShape_KB_NoCapAtTwelveBullets`. These are deliberate rename-with-semantic-flip — a future grep `BulletCap` no longer matches anything that asserts a cap, which is the correct end state.

### Brief rewrite for dual-shape S5 (7b400c97 + 25710bd1 + 1619ee19)

**Verdict: SOLID, with one observable trim.** The brief replaces the run-43 KB classification discriminator with the run-48 self-inflicted-reversible litmus. Plural audience, dual-shape teaching, empty-KB permission, 5 worked examples named verbatim from run-48 audit.

- 25710bd1 restored the URL-composition discard worked example (storage_apiHost vs storage_apiUrl + UnknownError) — kept pinned by `TestSynthesisWorkflow_KBURLCompositionDiscardExample`
- 1619ee19 trimmed the section to fit the 22000-token per-part ceiling — the trimmed version retains the three load-bearing anchors that the pin test asserts on

**Pin-test churn**: Three retired (`P1_KBDiscriminator…`, `P1_DiscardExampleStorageEndpoint`, `P1_XCacheRemovedFromKeepList`); four added (`KBSelfInflictedReversibleLitmus`, `KBDualShape`, `KBMayBeEmpty`, `KBPluralAudience`). Anchor migration tracks the substance rewrite.

### Worktree-isolation regression fix (289a8b34)

**Verdict: REAL BUG FIX, lands clean.** The `subagent_type=general-purpose` directive previously lived only inside the dispatched brief body (via `writePromptRecipeContext`). On disk-fallback / multi-file paths, the main agent receives only `BriefPath + Notice` and never reads the body — so the directive was invisible at dispatch time. Main agent defaulted to `subagent_type="claude"`, which in the VSCode-native Claude harness defaults to worktree isolation, which fails on the non-git recipe authoring `outputRoot` with `"Cannot create agent worktree: not in a git repository"`.

**Fix**: `mainAgentSubagentTypeGuidance()` appended to `r.Notice` on all three `handleBuildSubagentPrompt` exit paths (multi-file index, single-file disk-fallback, inline) + explicit `subagent_type="general-purpose"` rule added to dispatch shape in 6 phase-entry guides (scaffold/feature/finalize/codebase-content/env-content/refinement).

**Pin test**: `TestHandleBuildSubagentPrompt_NoticeCarriesSubagentTypeDirective` across all 8 brief kinds. Correct coverage.

**Why it matters**: This bug bites any recipe-engine dispatch that the Claude Code VSCode harness orchestrates and that lands on the disk-fallback path. Independent of v9.98.0 work, this fix should ship.

### Codex code-review cleanups (ff16698d + d907778b + 4f07b948)

- **ff16698d** (dual-anchor framing residue): Two surviving "Both reference recipes" lines still presented `laravel-showcase` as authoritative, contradicting the showcase demotion at L12. Now consistently name `laravel-jetstream` as the calibration anchor. Pure consistency repair.
- **d907778b** (`boldBulletRE` opt-in path documented): The `boldBulletRE` branch of `isSymptomFirstShape` was implicit opt-in — ANY `- **stem**` bullet flipped detection to shape (2) without requiring the explicit `### Gotchas` wrapper. Documenting the existing behavior rather than tightening the heuristic was the lower-churn fix (two existing tests deliberately use bare bold-stem inputs as shape-(2)). Added doc-comment on `isSymptomFirstShape` + spec note at §Surface 5.
- **4f07b948** (yaml-comment false-positive): `HasDirective` callbacks were raw-scanning yaml for directive presence — a `# `-commented directive read as "shipped" when the executable yaml didn't ship it. New `stripAllYAMLComments` helper applied to 5 callsites. Pin test added (`exposedHeaders mentioned only in yaml comment — NOT shipped`).

All three are codex-flagged cleanups with concrete reasoning and pin tests. Land clean.

---

## v9.98.0 merge — review

### R49-I1: rolling-deploy mechanism + `maxRam` (39d0053b)

**Files touched (15)**: `internal/recipe/content/briefs/env-content/per_tier_authoring.md` (PASS exemplar), `internal/knowledge/themes/refinement-references/{voice_patterns.md, refinement_thresholds.md, yaml_comments.md}`, `internal/recipe/content/briefs/refinement/derived_rules.md`, `internal/content/workflows/recipe.md`, `internal/knowledge/themes/{core,model,operations}.md`, `internal/ops/checks/worker_gotcha.go`, `internal/tools/workflow_checks_recipe.go`, plus refinement-relevant examples and the citation-map.

**Verdict: SOUND, multi-vector closure.** The fix is not just a text rewrite — it addresses all 5 vectors flagged in `run-49-stale-knowledge-findings.md §"Issue 1"`:
1. Canonical PASS exemplar (per_tier_authoring.md:301-317) rewritten with correct `temporaryShutdown: false`-as-mechanism framing
2. Anti-pattern-fix exemplar (per_tier_authoring.md:843-845) rewritten
3. Voice-pattern reference (voice_patterns.md:101) rewritten
4. `maxRam` adapt-knob hallucination removed; the new adapt-knob points at `minRam`/`minFreeRamGB` which the yamls actually set
5. Anti-repetition rule applied within the PASS exemplar shape

The `feedback_horizontal_scaling_vs_ha` memory's three-axis framing (throughput / HA-during-crash / rolling-deploy-cutover) is now reflected in the substrate: rolling-deploy is `temporaryShutdown` axis; `minContainers≥2` is the capacity/crash-tolerance axis. The third axis explicitly added.

### R49-I2: cross-service env model + envIsolation purge (2455aa3a)

**Files touched (30)**: Spec, brief, validators, atoms, ops env-shadow checks, classify, citations, refinement-2 briefs, content-lint tests, 10 atom-golden fixtures.

**Verdict: COMPREHENSIVE PURGE, lands clean.** Every envIsolation reference removed from porter-facing surfaces:
- `docs/spec-content-surfaces.md` — citation-table topic descriptor updated
- `internal/content/workflows/recipe/briefs/{writer/citation-map.md, editorial-review/citation-audit.md}` — `envIsolation semantics` row dropped
- `internal/recipe/citations.go:17` — `"envIsolation": "env-var-model"` mapping removed
- `internal/recipe/classify.go:151` — porter-facing classification path no longer gates on envIsolation
- `internal/recipe/briefs_refinement2.go:307` — citation-trigger pattern list cleaned
- `internal/recipe/content/briefs/scaffold/platform_principles.md` — "Managed services" + "same-key shadow trap" section fully rewritten away from auto-inject premise
- `internal/recipe/content/briefs/codebase-content/synthesis_workflow.md` — worked example rewritten ("same-key shadow trap" → "own-key alias rename"); the yaml-comment GOOD/BAD pair correctly inverted (Surface 7 self-contained vs cross-surface deferral)
- 10 atom-golden fixtures updated to the new env model

**Tool layer also updated**: `internal/tools/workflow_checks_generate.go`, `internal/ops/env_shadow.go`, `internal/ops/checks/env_self_shadow.go` track the new model — the operational env-shadow check stays correct under both legacy (envIsolation: none, zcp's own project) and default (envIsolation: service, porters') modes, because the check operates on actual env state observed in the container, not on inferred isolation mode.

**Tests**: `TestCheckEnvRefs_*`, `TestCheckEnvSelfShadow_*`, `TestEnvGenerateDotenv_*` pass against the new fixtures.

**Spec invariant promoted**: The `CLAUDE.md` invariant *"`run.envVariables` is the canonical setup-entry env-var location"* (which was already on main) now has the matching porter-facing brief/atom/citation alignment.

### R49-I3: `zsc noop --silent` retirement + gate refactor (cdbcc0da)

**Files touched (33)**: All the dev-runtime atoms and briefs, the worker-dev-server gate + its tests, deploy-subdomain logic, topology/runtime_class, 6 atom-golden fixtures.

**Verdict: LARGEST DIFF, MOST CAREFUL.** This is the most surgical of the four corpus closures:
- All 8 atoms updated to omit `run.start` for dev mode (was: `zsc noop --silent`)
- `internal/recipe/content/principles/dev-loop.md` rewritten
- `internal/recipe/gate_worker_dev_server.go` refactored (135 LoC change + 100 LoC test change) to predicate on **dynamic-runtime base** rather than the literal `zsc noop --silent` string — this is the load-bearing refactor; the gate now correctly enforces dev-server attestation for dynamic runtimes regardless of whether they ship `run.start` or omit it
- `internal/tools/deploy_subdomain.go` (+ test) updated to handle the new dev-runtime shape
- `internal/topology/runtime_class.go` runtime-class enum updated

**Cross-cutting check vs v9.99.0**: This commit touches `internal/recipe/gate_worker_dev_server.go` while v9.99.0 adds `internal/recipe/gate_kb_self_inflicted_reversible.go`. Both gates register through `internal/recipe/gates.go` — the gate-registration code path is shared. **The merge auto-merged `gates.go` cleanly**; both gates land on the registered-gates list. No interference.

**The KB-bullet regression in run-49 (appdev README.md:199-210 "Dev container starts but the Vite dev server doesn't")** — this v9.98.0 commit's atom + brief rewrites tell sub-agents to omit `run.start` AND retire the narration that generated the KB bullet. The bullet shape should not recur post-merge.

### R49-I4: execOnce burn-recovery scoping (cc021a34 + 0eea18c7)

**Files touched (6)**: `internal/content/workflows/recipe.md`, `internal/content/workflows/recipe/phases/deploy/init-commands.md`, `internal/recipe/atoms_derived_rules_run43_test.go` (+44 LoC new tests), `internal/recipe/content/briefs/refinement/derived_rules.md`, `internal/tools/workflow_checks_claude_md_test.go`, `internal/tools/workflow_checks_worker_correctness_test.go`.

**Verdict: PRECISE SURGICAL FIX.** The init-commands.md "Recovering a burned execOnce key" section now scopes burn-recovery teaching to **static keys only** (`bootstrap-seed`, `<slug>.<op>.v1`); per-deploy keys (`${appVersionId}-*`) are explicitly held out from this teaching. The fixture conflations in two test files (workflow_checks_claude_md_test.go, workflow_checks_worker_correctness_test.go) — which previously taught the mixed key-shape + burn-recovery framing as GOOD pattern — are corrected.

**0eea18c7 minor follow-up**: removes literal version-anchor tokens from the burn-recovery atom (the atom now reads cleanly without "v1"/"v2" examples that could be misread as per-deploy version-id usage).

**New test surface**: `atoms_derived_rules_run43_test.go` adds 44 LoC of pin tests asserting that the burn-recovery section's prose-pattern set does NOT match per-deploy-key paragraphs.

**Run-49 verbatim regression** (apidev/README.md:338 "the migrator gets `ECONNREFUSED`, the key burns") — the brief edits and the test-fixture corrections should prevent this verbatim shape from recurring post-merge.

### Theme & gate alignment (fcec3818 + 8f8434b8)

- **fcec3818**: Test-fixture rewrites pulling existing fixtures (which baked the stale `zsc noop` dev-start pattern into expected outputs) into alignment with the new omit-run.start convention. Pure mechanical sync.
- **8f8434b8**: Gate registration comment alignment + core knowledge theme update so the gate's doc comment and the knowledge-theme that referenced it both speak the new convention. Pure doc/comment alignment.

Both are post-cleanup commits — the load-bearing change landed in cdbcc0da.

---

## Conflict resolutions — review

Two real content conflicts surfaced when merging v9.98.0 on top of v9.99.0 (the dry-run against main was conflict-free because v9.99.0 was tested in isolation; chaining produces the real conflicts).

### Conflict #1: `docs/spec-content-surfaces.md` §Surface 5 Anti-pattern paragraph

**HEAD side (v9.99.0)**: Anti-pattern paragraph cites the OLD env-var-model — "cross-service vars auto-inject project-wide — never declare `key: ${key}` at all." This is the wrong model that v9.98.0 was specifically authored to correct.

**Incoming side (v9.98.0)**: Adds a Citation-rule paragraph (duplicate of one already at line 246, just narrower wording) + updated Anti-pattern with the CORRECT env-var-model — "cross-service vars require explicit `run.envVariables` aliases (the value is not in the process env without the alias); same-key declaration on a project-level var produces a literal-string shadow."

**Resolution**: Kept v9.99.0's broader Citation rule (already at line 246), dropped the duplicate, used v9.98.0's CORRECT Anti-pattern text. **The v9.99.0 spec rewrite's content stands; only the wrong env-model reference within it is replaced with v9.98.0's correct one.** This is exactly the resolution the underlying intents demand: v9.98.0's whole purpose is fixing the env model, so any line citing that model gets v9.98.0's version.

### Conflict #2: `synthesis_workflow.md` lines 641-653

**HEAD side (v9.99.0)**: New section "KB may be empty — that's a positive signal" — the empty-permission teaching from the Surface 5 dual-shape rewrite.

**Incoming side (v9.98.0)**: A fragment ("(i) Walk the body, not the stem") from the OLD KB classification discriminator that v9.99.0 commit `7b400c97` deliberately retired ("Replaces the run-43 KB classification discriminator with the run-48 recalibration's load-bearing filter: refuse KB content whose symptom fires only when the porter UNDOES a directive the recipe ships.").

**Resolution**: Kept v9.99.0's new section, dropped v9.98.0's fragment. **The v9.98.0 edit was to a section the v9.99.0 author intentionally deleted.** Applying it would re-introduce dead code — the discriminator's other clauses (ii/iii/iv) are also gone from v9.99.0, so a standalone "(i)" would be a confusing orphan. Auto-merge handled the rest of v9.98.0's edits to this file (lines 155-200 — the same-key→own-key rewrite, lines 256+ — the `zsc noop`→`deployFiles: [.]` PASS rewrite); only the dead discriminator clause needed manual drop.

**Verification commands**:
```
$ grep -n auto-inject internal/recipe/content/briefs/codebase-content/synthesis_workflow.md
   (empty — R49-I2 fix landed)
$ grep -n "zsc noop" internal/recipe/content/briefs/codebase-content/synthesis_workflow.md
   (empty — R49-I3 fix landed)
$ grep -n "Cross-service vars don't reach\|deployFiles: \[\.\]" internal/recipe/content/briefs/codebase-content/synthesis_workflow.md
177:- `zerops.yaml` comment (8 lines): *"Cross-service vars don't reach
263:**PASS** (laravel-showcase apidev/zerops.yaml dev `deployFiles: [.]`):
266:# `deployFiles: [.]` ships the whole source tree on every dev deploy
```

Both v9.98.0 intents preserved. v9.99.0 dual-shape teaching preserved.

---

## Cap bump — review

Commit `a7e0c386`. Refinement brief 50→52 KB.

**Mechanism**: Both branches added derived-rules.md content (v9.98.0: +8/6/2 lines across 3 commits; v9.99.0: +2 lines). Combined, the refinement brief lands at 51,426 bytes (51.4 KB), 226 bytes over the prior 50 KB cap.

**Historical pattern**: The cap was last raised at run-43 F7 (48→50 KB). Prior raises at runs 32, 33, 34, 43 F1, 43 F5/F6. Every raise documented inline in the test doc-comment. The new entry follows the same template.

**Headroom**: 52 KB cap, current ~51.4 KB → 0.6 KB headroom. Tighter than ideal; the next legitimate substrate addition will likely require another bump. Considered raising to 54 KB but kept conservative since the cap is a regression guard, not a hard limit.

**Alternative considered + rejected**: Compressing existing derived-rules.md content to fit 50 KB. Rejected because all current content is load-bearing (run-43 F-anchor rules, run-49 corrections). Compressing would weaken the substrate; raising the cap is the lower-risk path.

---

## Cross-cutting checks

### Gate registration

Both branches add new gates. Auto-merge of `internal/recipe/gates.go` was clean. Combined registration:
- v9.99.0: `kb-self-inflicted-reversible` (codebase-content gates list)
- v9.98.0: existing gate refactors at `gate_worker_dev_server.go` (test/registration unchanged; the refactor is internal to the gate logic)

No collision. Both gates run in their own scopes.

### Atom-golden fixtures

10 atom-golden files were auto-merged (`internal/workflow/testdata/atom-goldens/develop/*.md`). Both branches edited the same files — v9.99.0 modified them as part of Surface 5 dual-shape work (handful of bytes each); v9.98.0 modified them more substantially for R49-I3 (removing `zsc noop` references). Auto-merge applied both sets. All `internal/workflow/...` tests pass post-merge.

### Lint stamp

`go test ./tools/lint/... -short` passes. `make lint-local` not run (would also verify atom-tree gates) — recommend running before pushing.

### Backward compatibility

All public types/functions on the merged branch maintain prior signatures. No breaking changes at the tool-handler boundary (`internal/tools/*`) or the platform/topology boundary. The new gate is a blocking validator — it can refuse content, but it does not introduce new error codes the agents need to learn (refusal carries an existing-shape Violation).

---

## Risk assessment

| Risk | Severity | Notes |
|---|---|---|
| Surface 5 spec rewrite ships before re-verification at recipe surface | **MEDIUM** | The spec change is correct on its own terms but it's been 3 days since last work on the branch. Recommend running a recipe dispatch against the merged engine before tagging as a new release. |
| Refinement-brief cap room is tight (~600 bytes) | **LOW** | Next substrate addition will need another bump; document it next time. |
| `kb-self-inflicted-reversible` gate not yet exercised in production | **LOW** | Gate is pin-tested, but no run between v9.99.0 and now has dispatched against it. First real run will be the first production test. |
| Old atom-golden tests baked stale dev-start patterns | **MITIGATED** | fcec3818 swept the fixtures; tests green. |
| Run-49's four regression shapes recur post-merge | **LOW** | The corpus edits are comprehensive (see v9.98.0 review above). Recommend re-dispatching nestjs-showcase as a confirmatory test. |
| v9.99.0's worktree-isolation fix interacts with v9.100.x's multi-agent-container work | **UNVERIFIED** | v9.100.x added Codex/Gemini/Antigravity/Cursor adapters. The worktree-isolation fix (289a8b34) hardens the Claude-side dispatch shape; the multi-agent adapters touch different code paths. Quick grep finds no overlap, but worth confirming with a manual `git log v9.99.0..HEAD -- internal/tools/` walk before pushing. |

---

## Recommendation

1. **Land on main** by merging the reconcile branch with `--no-ff` (preserves the v9.98 + v9.99 merge points as deliberate decisions in main's history). The branch is test-clean across all packages.

2. **Tag v9.101.0 after merge**, mentioning both v9.98.0 + v9.99.0 in the tag annotation so the lineage gap is visible from `git tag --list -n`.

3. **Re-dispatch nestjs-showcase** as a confirmatory run. The four R49 failure shapes should all be absent; the new `kb-self-inflicted-reversible` gate should fire on any sub-agent that drafts a self-inflicted-shape bullet; the Surface 5 dual-shape should manifest as either H3 operational sections or `### Gotchas` bullets per content (not template default).

4. **Add a release-gate test** (separate follow-up): a CI check that the next minor-version bump cannot tag from a parent that lacks any known-fix-branch tag. This is the structural-prevention layer for the release-management gap that caused this whole exercise — the gap is now closed for v9.98/v9.99, but the same shape could recur on a future side-branch.

5. **Delete or archive the side branches** (`corpus-recalibration-2026-05-20`, `kb-surface-recalibration`) once the merge lands, with their tags preserved. Standard "merged feature branch cleanup."

### What is explicitly NOT recommended

- Do not rebase the reconcile branch onto main with linear-history — the `--no-ff` merges are deliberate provenance markers.
- Do not push without running `make lint-local` first.
- Do not amend either of the two merge commits after pushing — their merge points are reference-able.

---

## Codex validation

Not run on this code review — every claim above is anchored to direct file-content quotes + grep verifications you can re-run. If you want an independent verifier pass, the prior validation agent `a0d15b5f1460c1873` is still warm and could be asked to spot-check three or four claims at random.
