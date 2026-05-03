# Plan — Top 3 flow-eval friction fixes

Date: 2026-05-03
Source: behavioral eval suite `20260503-144814` (6/6 ok, but consistent friction across retrospectives)
Pressure-tested by: Codex (session `019dee9c-f788-7b82-842a-761c98f6c3c4`)

## Goals

| Issue | Pre-state | Post-state |
|---|---|---|
| 1. routeOptions trap | `adopt zcp` ranked #1; recipes with wrong DB at confidence 0.95 | self-host filtered; recipes with incompatible deps demoted/dropped |
| 2. Plan-shape trap | flatten silently dropped → misleading standard-mode error | flatten hard-rejected with actionable hint; `bootstrapMode` required enum |
| 3. Develop firehose | 42/51 develop-active atoms emit for all runtime classes | runtime-mechanic atoms tagged with `runtimes:` axis; pinned by lint |

Final gate: re-run `flow-eval all` post-implementation, confirm retrospectives no longer flag the three friction points.

## Sequencing

1. **Plan contract (Issue 2)** — defines plan JSON shape; downstream routes (recipe/classic/adopt) all submit it.
2. **Route discovery (Issue 1)** — depends on stable plan shape; ordering + filtering decide what plan agent produces.
3. **Atom retag (Issue 3)** — atom golden snapshots churn last; route/plan must be stable first to avoid double-churn.

## Phase 1 — Plan contract

### 1.1 Spec drift cleanup

`docs/spec-workflows.md:457` says `standard derives {prefix}stage from a dev suffix`, which contradicts line 87 (`explicit stage, no hostname-suffix derivation`) and validate_test.go:151. Edit: remove the derivation language.

### 1.2 Custom UnmarshalJSON on BootstrapTarget

Add `UnmarshalJSON` to `internal/workflow/validate.go::BootstrapTarget` that hard-rejects flattened RuntimeTarget fields at the top level.

Rejected keys: `bootstrapMode`, `stageHostname`, `devHostname`, `type`, `isExisting`.

Tests (RED → GREEN):
- `TestBootstrapTarget_UnmarshalJSON_RejectsFlattenedBootstrapMode`
- `TestBootstrapTarget_UnmarshalJSON_RejectsFlattenedStageHostname`
- `TestBootstrapTarget_UnmarshalJSON_RejectsFlattenedDevHostname`
- `TestBootstrapTarget_UnmarshalJSON_AcceptsCorrectlyNestedShape`

### 1.3 bootstrapMode required enum

Make `runtime.bootstrapMode` required on every runtime target (no empty→standard default).

Changes:
- `EffectiveMode()` — drop empty→standard mapping.
- Validate loop — reject empty mode with `runtime.bootstrapMode is required: dev, simple, or standard`.
- Standard-without-stage error — append `for a single mutable dev container use runtime.bootstrapMode="dev"`.

Tests (RED → GREEN):
- `TestValidateBootstrapTargets_EmptyMode_RejectsWithEnumHint`
- `TestValidateBootstrapTargets_StandardWithoutStage_HasActionableSuggestion`

### 1.4 Tool description JSON example

`internal/tools/workflow.go:37` plan parameter description — replace the lying `INVALID_PARAMETER` text with real error text. Add literal JSON examples for both single-dev and dev/stage-pair shapes.

### 1.5 Atom sync

`grep -rn bootstrapMode internal/content/atoms/` — update any atom teaching old empty→standard semantics.

### Phase 1 verification gate

- Unit: `go test ./internal/workflow/... -race`
- Tool: `go test ./internal/tools/...`
- Integration: bootstrap path
- E2E (after all phases): flow-eval `classic-go-simple`

## Phase 2 — Route discovery

### 2.1 Self-host filter in adoptableServices

`internal/workflow/route.go:206-227` — add `runtime.Info`-based filter. Match `ServiceID` first, fall back to hostname only if ID unavailable. Pattern lives at `compute_envelope.go:189` and `workflow_route.go:35`.

Tests:
- `TestAdoptableServices_ExcludesSelfByServiceID`
- `TestAdoptableServices_FallsBackToHostnameWhenNoServiceID`
- `TestAdoptableServices_LocalModeNoSelf_KeepsAllNonSystem`

### 2.2 Stack-mismatch filter

New file `internal/workflow/intent_dependencies.go`:
- `ExtractIntentDependencies(intent string) []string`
- `RecipeServiceTypes(yamlBytes []byte) []string`
- `CompareStacks(intent, recipe []string) StackMismatch{Contradicted, MissingFromRecipe, Extras}`

Wire into `BuildBootstrapRouteOptions`:
- `Contradicted` non-empty → drop recipe.
- `MissingFromRecipe` non-empty → demote below classic.
- `Extras` non-empty → flag in `recipeWhy()`.

Boundary: workflow-layer enforcement of route viability against explicit user constraints. Confidence-mutation stays out of scope (that's recipe-team).

### 2.3 Reorder adopt below recipes (conditional)

`route.go:285-287` — when no resume + recipes with confidence ≥ 0.85 exist → `recipe → adopt → classic`. Otherwise `adopt → classic`.

`route_test.go:140` — explicitly update pin: `TestBuildBootstrapRouteOptions_RecipeAboveImplicitAdopt_WhenRecipesPresent`.

Spec edit: `docs/spec-workflows.md` — describe conditional ordering.

### 2.4 recipeWhy surfaces extras

`route.go:315-323` — when `StackMismatch.Extras` non-empty, prefix description with `Over-provisions: adds [...] not in user intent`.

### Phase 2 verification gate

- Unit + tool + integration green.
- E2E (after all phases): flow-eval `classic-php-mariadb-standard`.

## Phase 3 — Atom retag + corpus lint

### 3.1 lintAxisRuntime in atoms_lint.go

Predicate: for each atom, if `phases` contains `develop-active` AND `runtimes` empty AND body (incl. code fences) matches `/run\.start|run\.ports|healthCheck|zsc\s+noop|zerops_dev_server/` → violation.

Tests on fixture atoms:
- `TestLintAxisRuntime_FailsOnDevelopActiveWithRunStartNoRuntimes`
- `TestLintAxisRuntime_PassesOnUniversalAtom`
- `TestLintAxisRuntime_PassesOnTaggedAtom`
- `TestLintAxisRuntime_ScansCodeFences`

### 3.2 Mass-tag sweep

Process:
1. Run lint to get violation list.
2. Classify each by content (dynamic / implicit-webserver / both).
3. Tag in chunks of 7 atoms; verify tests after each chunk.

Codex-flagged concrete misses:
- `develop-checklist-simple-mode` → `runtimes: [dynamic, implicit-webserver]`
- `develop-close-mode-auto-standard` → `runtimes: [dynamic, implicit-webserver]`
- `develop-first-deploy-verify` → `runtimes: [dynamic, implicit-webserver]`
- `develop-platform-rules-container` → keep `environments: [container]` + add `runtimes: [dynamic]`

Universal atoms (legitimately untagged):
- API error handling, auto-close semantics, close-mode taxonomy, git-push setup, deploy-files self-deploy warning, env-var channels, first-deploy intro/execute/env-vars/scaffold, strategy awareness/review, mode expansion, verify matrix.

Distinguishing rule: universal atoms describe workflow state, tool response reading, deploy strategy. Missed atoms mention start mechanics, health checks, dev servers, ports, zsc noop.

### Phase 3 verification gate

- `TestAtomLint*` green.
- Lint passes on real corpus (no violations).
- Tool tests + integration develop-briefing green.
- E2E (after all phases): flow-eval `classic-static-nginx-simple` — narrower response.

## Risk register

| Risk | Likelihood | Mitigation |
|---|---|---|
| Required enum breaks recipe-route plans | Low | bootstrap_guide_assembly.go:239 already requires mode for recipe targets. |
| Test fixtures with empty mode break | High but predictable | Sweep fixtures explicitly. |
| Intent token extraction false positives | Medium | Conservative match (require requirement-style phrasing). |
| Route ordering change cascades into other tests | Low | Re-run flow-eval after Phase 2 as gate. |
| Golden snapshot churn after atom retag | High but planned | Sweep in chunks of 7; snapshot update per chunk. |

## Boundary

Workflow-team scope (touchable):
- `internal/workflow/route.go`, `validate.go`, `compute_envelope.go`, `synthesize.go`
- `internal/tools/workflow.go`, `workflow_develop.go`, `workflow_bootstrap.go`
- `internal/content/atoms/*.md` (corpus content), `internal/content/atoms_lint.go`
- `docs/spec-workflows.md`

Recipe-team scope (mention only, no edits):
- `internal/recipe/`
- `internal/tools/workflow_recipe.go`, `workflow_checks_recipe.go`
- `internal/content/workflows/recipe/`
- `internal/knowledge/recipe_matcher.go`
- `internal/knowledge/recipes/*.import.yml`

If stack-scoring needs structured metadata exposure long-term, that's a recipe-team change — workflow consumes, doesn't author.

## Success criteria

1. `flow-eval all` re-run: retrospectives no longer flag adopt-zcp / standard-mode-trap / develop-firehose.
2. `make lint-local` green with new axis-runtime lint pin.
3. `go test ./... -race` green across all 4 layers.
4. CI release pipeline green (full strict lint + race tests).
5. `docs/spec-workflows.md` internally consistent.
