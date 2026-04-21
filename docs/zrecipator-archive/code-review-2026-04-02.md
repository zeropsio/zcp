# Code Review — Recipe Workflow System (2026-04-02)

Branch: `ar-zrecipator` | Scope: unstaged changes (10 modified + 26 new files)

Build: compiles clean. `go vet`: clean.

---

## CRITICAL — 0 issues

No data corruption or security vulnerabilities found.

---

## HIGH — 7 issues

### H1. Leaked `signal.NotifyContext` cancel — goroutine leak (3 occurrences)

`cmd/zcp/eval.go:151`, `cmd/zcp/eval.go:205`, `cmd/zcp/eval.go:325`

`ctx, _ := signal.NotifyContext(...)` discards the stop function. Per Go docs this leaks a goroutine + OS signal channel per call. Fix: `ctx, stop := signal.NotifyContext(...); defer stop()`.

### H2. `ResolveBuildBase` is entirely dead logic

`internal/workflow/recipe_decisions.go:32-46`

All three branches return `runtimeType` unchanged. The `needsNodeBuild` parameter has no effect. This function is a no-op. Either the differentiated logic hasn't been implemented yet, or it should be removed.

### H3. Dead compiled regexes `extractStartRe` / `extractEndRe`

`internal/tools/workflow_checks_recipe.go:40-41`

Package-level compiled regexes declared but never referenced. `extractFragmentContent` uses `strings.Index` with literal markers instead. Remove dead code.

### H4. Custom `contains()` reimplements `strings.Contains`

`internal/eval/recipe_create.go:252-259`

Unnecessary reimplementation. Replace with `strings.Contains`. Same issue in test files: `containsSubstring`/`findSubstring` in `internal/workflow/recipe_validate_test.go:293-305` and `contains`/`findInString` in `internal/tools/workflow_checks_finalize_test.go:231-242`.

### H5. `strings.Title` used with nolint when `titleCase()` helper exists

`internal/workflow/recipe_templates.go:57`

Uses deprecated `strings.Title` with `//nolint:staticcheck` suppression. A `titleCase()` helper already exists at `internal/workflow/bootstrap_guide_assembly.go:168`. Use the existing helper instead.

### H6. `TestGetWorkflow_AllWorkflows` not updated for "recipe"

`internal/content/content_test.go`

`TestListWorkflows_Complete` was updated to include "recipe", but `TestGetWorkflow_AllWorkflows` still only tests `bootstrap`, `deploy`, `cicd`. Missing coverage for `GetWorkflow("recipe")`.

### H7. Missing test coverage for `SkipStep` error paths

`internal/workflow/recipe_test.go`

`CompleteStep` error paths are covered (all-done, wrong name, not active, short attestation — 4 test cases). However, `SkipStep` is missing tests for: not active (`!r.Active`), wrong step name. These are guarded in source but untested.

---

## MEDIUM — 10 issues

### M1. Files exceed 350 LOC limit (3 files)

| File | Lines | Over by |
|------|-------|---------|
| `cmd/zcp/eval.go` | 414 | 64 |
| `internal/workflow/engine.go` | 397 | 47 |
| `internal/tools/workflow_checks_recipe.go` | 376 | 26 |

### M2. `buildWorkflowHint` missing `deploy` case

`internal/server/instructions.go:105-123` — Refactored to switch with `bootstrap` and `recipe` cases, but no `case "deploy":`. Pre-existing gap made more visible by the restructuring.

### M3. `detectActiveWorkflow` defaults to bootstrap when nothing is active

`internal/tools/workflow.go:218` — When session exists but no workflow state has `Active == true`, falls back to `workflowBootstrap`. Could cause bootstrap operations on non-bootstrap sessions. Safer to return `""`.

### M4. Stale error message excludes "recipe"

`internal/tools/workflow.go:157` — Error says `"Valid workflows: bootstrap, deploy, cicd"` — missing "recipe".

### M5. UTF-8 truncation risk in `buildPriorContext`

`internal/workflow/recipe.go:283` — Byte-level truncation at 77 chars can split multi-byte UTF-8 characters.

### M6. `showcaseMissing` only checks 3 of 6 showcase fields

`internal/workflow/recipe_validate.go:110-126` — `storageDriver`, `searchLib`, `mailLib` are unchecked. If these fields are optional, this is fine but should be documented.

### M7. Non-atomic temp file pattern in `WriteRecipeMeta`

`internal/workflow/recipe_meta.go:37-43` — Uses predictable temp name unlike `saveSessionState` which uses `os.CreateTemp`.

### M8. `checkRecipeGenerate` / `checkRecipeFinalize` return `nil, nil` on nil plan

`internal/tools/workflow_checks_recipe.go:52-53`, `internal/tools/workflow_checks_finalize.go:18` — Both return `nil, nil` on nil plan. Caller at `engine_recipe.go:56` guards with `if result != nil`, so no panic in practice — but the pattern is fragile if new callers are added without nil-checks.

### M9. `map[string]any` for vertical autoscaling

`internal/tools/workflow_checks_finalize.go:98` — Convention prohibits `any` when concrete type is known. Autoscaling fields are well-defined.

### M10. No test file for `recipe_create.go`

`internal/eval/recipe_create.go` — TDD mandate requires tests. `BuildRecipeCreatePrompt` and `checkCreateSuccess` are easily unit-testable.

---

## LOW — 8 issues

| # | Location | Issue |
|---|----------|-------|
| L1 | `internal/workflow/recipe_guidance.go:12` | `env Environment` parameter accepted but never used (dead param) |
| L2 | `internal/workflow/recipe_guidance.go:43-44` | Silent empty return on content load failure, no diagnostic |
| L3 | `internal/workflow/recipe_steps.go:15` | `recipeStepDetails` is mutable package var (matches bootstrap pattern but technically violates "no global mutable state") |
| L4 | `internal/workflow/recipe_templates_test.go:76-170` | YAML assertions via `strings.Contains` — fragile to formatting changes |
| L5 | Multiple test files | `TestWriteReadRecipeMeta`, `TestCommentRatio`, `TestExtractFragmentContent` don't follow `Test{Op}_{Scenario}_{Result}` naming |
| L6 | `internal/tools/workflow_checks_recipe.go:56` | `filepath.Dir(filepath.Dir(stateDir))` assumes exactly 2-level nesting for project root |
| L7 | `internal/eval/recipe_create.go:242-250` | Success detection via string matching ("Recipe complete") is brittle |
| L8 | `internal/content/workflows/recipe.md:133` | Uses `zerops.yaml` but checker uses `ParseZeropsYml` — inconsistent naming |

---

## Top 5 Actionable Fixes (by impact)

1. **Fix `signal.NotifyContext` leak** (H1) — 3-line fix, prevents goroutine leaks
2. **Remove dead `ResolveBuildBase`** (H2) — or implement the intended differentiation
3. **Delete dead regexes + custom `contains()`** (H3, H4) — dead code cleanup
4. **Split 3 files over 350 LOC** (M1) — convention compliance
5. **Add missing test cases** (H6, H7, M10) — coverage gaps
