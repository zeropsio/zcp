# Bootstrap Flow Revision -- Pragmatist Implementation Plan

## Validation Summary

All claims in the analysis (`analysis-bootstrap-revision.md`) have been verified against the actual source code:

| Claim | Verified | Notes |
|-------|----------|-------|
| File line counts (18 files) | All match within 1 line | Accurate as of 2026-03-04 |
| `PlannedService` references | 7 files total | workflow/{validate,engine,bootstrap}_test.go + tools/workflow.go + validate.go + engine.go |
| `ServicePlan` references | 7 files total | Same files + bootstrap.go + plans/ |
| `BootstrapComplete` signature lacks `context.Context` | Confirmed | `engine.go:126` -- `BootstrapComplete(stepName, attestation string)` |
| `autoCompleteBootstrap` always `Failed=0` | Confirmed | `bootstrap_evidence.go:47` -- `ev.Failed` never set above 0 |
| C5 import bug (`ss.Error` silently dropped) | Confirmed | `import.go:105-115` -- loop over `result.ServiceStacks` only extracts Processes, never checks `Error *APIError` |
| `KnowledgeTracker.IsLoaded()` is boolean, not per-type | Confirmed | `knowledge_tracker.go:41` -- `len(kt.briefingCalls) > 0 && kt.scopeLoaded` |
| `RegisterWorkflow` missing `logFetcher` parameter | Confirmed | `server.go:89` -- no logFetcher passed |
| `checkHTTPHealth` hits `/health` (redundant) | Confirmed | `verify.go:147` -- `checkHTTPHealth(ctx, httpClient, subdomainURL+"/health")` |
| All 14 packages pass tests | Confirmed | `go test ./... -count=1 -short` -- all green |

---

## Incremental vs Atomic Assessment

The analysis proposes 15 items in dependency order. After examining the actual code, here is what can be done independently and what forms atomic clusters.

### Truly Independent (can ship and test in isolation)

1. **C5: Import error surfacing** -- Pure bug fix in `ops/import.go`. No type changes. No interface changes. Can ship, deploy, test immediately.

2. **Verify speedup (F+G)** -- Batching log fetches, parallelizing HTTP checks, replacing `/health` with `/` in `verify.go` and `verify_checks.go`. Self-contained in ops package. No workflow dependency.

3. **Build polling speedup** -- Three constants in `ops/progress.go`. Trivial.

4. **H10: KnowledgeTracker per-type tracking** -- Isolated to `ops/knowledge_tracker.go` + its test. Changes `IsLoaded()` to `IsLoadedFor(runtimeType string)`. Callers: `injectKnowledgeHint` in `tools/workflow_bootstrap.go` (minor adjustment).

5. **Stage validation in deploy_validate.go** -- Adding a warning for `start: zsc noop --silent` on stage entries. Self-contained in `ops/deploy_validate.go`.

### Atomic Cluster 1: Type System Change (must be one commit)

Items: A (BootstrapTarget types) + parts of B (lifecycle) + removing PlannedService

This is the highest-risk change. `PlannedService` appears in:
- `workflow/validate.go` (definition + `ValidateServicePlan`)
- `workflow/engine.go` (`BootstrapCompletePlan` accepts `[]PlannedService`)
- `workflow/bootstrap.go` (`ServicePlan.Services`, `validateConditionalSkip`)
- `workflow/bootstrap_test.go` (test construction)
- `workflow/engine_test.go` (test construction)
- `workflow/validate_test.go` (test construction)
- `tools/workflow.go` (`WorkflowInput.Plan []workflow.PlannedService`)

All 7 files must be updated atomically. The test blast radius is ~1145 lines of test code.

### Atomic Cluster 2: Step Consolidation (must be one commit)

Items: C (11 -> 5 steps) + H6 (skip guard constants)

Files that must change together:
- `workflow/bootstrap_steps.go` (step definitions: 276 lines, full rewrite)
- `workflow/bootstrap.go` (step constants, `validateConditionalSkip`, `NewBootstrapState`)
- `workflow/bootstrap_evidence.go` (evidence map keyed by old step names)
- `workflow/bootstrap_test.go` (all tests reference 11-step model)
- `workflow/engine_test.go` (tests reference step names like "plan", "detect")
- `workflow/bootstrap_guidance.go` + `content/workflows/bootstrap.md` (section tags)
- `tools/workflow_bootstrap.go` (routes `input.Step == "plan"` to plan handler)

This changes the entire bootstrap model. Cannot be incremental.

### Atomic Cluster 3: Hard Checks (depends on Cluster 1 + 2)

Items: D (StepChecker) + H1 (context.Context)

Files that must change:
- `workflow/engine.go` (`BootstrapComplete` signature change)
- New: `workflow/bootstrap_checks.go`
- New: `tools/workflow_checks.go`
- `tools/workflow_bootstrap.go` (pass checker to engine)
- `server/server.go` (pass `logFetcher` to `RegisterWorkflow`)
- `tools/workflow.go` (`RegisterWorkflow` signature)

### Depends on Cluster 3

- E: Batch verify (`VerifyAll`) -- needs hard checks to be useful
- K: Per-service decision metadata -- needs new plan structure
- I: CLAUDE.md reflog -- needs new plan structure
- H2: BuildInstructions routing fix -- needs new project state detection
- L, M, J: Content/guidance changes -- need 5-step model

---

## Risk Assessment

### Risk Matrix

| Change | Risk | Impact if broken | Rollback ease |
|--------|------|-----------------|---------------|
| C5: Import error surfacing | **LOW** | Import errors still silently dropped (status quo) | Revert 1 file |
| Verify speedup | **LOW** | Verify produces wrong results (tests catch) | Revert 2 files |
| Polling speedup | **NEGLIGIBLE** | Polls faster (harmless worst case) | Revert 3 constants |
| H10: KnowledgeTracker per-type | **LOW** | Knowledge hint wrong (cosmetic) | Revert 1 file |
| Stage validation | **LOW** | Missing warning (status quo) | Revert 1 file |
| **Type system change (Cluster 1)** | **HIGH** | `zerops_workflow action="complete" step="plan"` breaks entirely | Revert 7 files, complex |
| **Step consolidation (Cluster 2)** | **HIGH** | All bootstrap flows break | Revert 8+ files, complex |
| **Hard checks (Cluster 3)** | **MEDIUM** | Bootstrap complete fails | Revert 4 files |
| Batch verify | **LOW** | Single-service verify still works | Revert 2 files |
| Decision metadata | **LOW** | Files not written (no user impact) | Revert 1 new file |
| CLAUDE.md reflog | **LOW** | Entry not appended (no user impact) | Revert 1 new file |

### Hidden Dependencies the Analysis Missed

1. **Integration tests reference step names**: `integration/bootstrap_conductor_test.go` and `integration/bootstrap_realistic_test.go` will break when steps change from 11 to 5. The analysis only lists unit test blast radius.

2. **`injectKnowledgeHint` hardcodes step name**: `tools/workflow_bootstrap.go:95` checks `resp.Current.Name != "load-knowledge"`. When steps consolidate, this step no longer exists. The knowledge hint logic must be updated or removed.

3. **`populateStacks` injected into every bootstrap response**: `tools/workflow_bootstrap.go` calls `populateStacks` on every response. After step consolidation, the discover step absorbs this role. The injection pattern stays but step awareness changes.

4. **`WorkflowInput.Plan` JSON schema description**: `tools/workflow.go:34` has a jsonschema tag referencing `{hostname, type, mode?}`. After type change, schema becomes `{runtime: {devHostname, type}, dependencies: [{hostname, type, resolution}]}`. MCP clients cache tool schemas -- stale schemas cause plan submission failures.

5. **`bootstrap.md` section tags**: `bootstrap_guidance.go` uses `extractSection()` to match step names to `<section name="...">` tags in `bootstrap.md`. Step name changes require synchronized `bootstrap.md` section tag updates. The analysis mentions this but does not list it as a dependency of Cluster 2.

6. **`isManagedService()` vs `isManagedTypeWithLive()`**: Both exist and are used differently. `validateConditionalSkip` uses `isManagedService()` (static prefixes). `ValidateServicePlan` uses `isManagedTypeWithLive()` (live + fallback). After introducing `Dependency.Resolution`, the skip logic changes meaning -- it now depends on whether any target has `Runtime.IsExisting=false` rather than "has runtime services."

7. **`StepContext.Attestations`**: `bootstrap.go:228-243` builds prior context from step attestations. The hard check model replaces attestations with `StepCheckResult`. `PriorContext` needs updating or the LLM loses visibility into what passed/failed in prior steps.

---

## Proposed Implementation Order (differs from section 12)

The analysis orders by dependency. I order by risk-adjusted value delivery. Rationale: ship independently-testable improvements first, build confidence, then tackle the atomic clusters.

### Phase 0: Low-Risk Independent Fixes (1-2 sessions)

**Deploy + test after each item.**

#### 0a. C5: Import error surfacing

File: `internal/ops/import.go`

Change: In the `for _, ss := range result.ServiceStacks` loop (line 105), check `ss.Error` and collect errors. Add `ServiceErrors` field to `ImportResult`.

Test: `internal/ops/import_test.go` -- add table case for service with Error set, verify it appears in result.

Risk: LOW. Failure mode = status quo (errors still dropped).

Build+Deploy+Test:
```
go test ./internal/ops/... -run TestImport -v
go build -o bin/zcp ./cmd/zcp
# Deploy to zcpx, test: import a service with invalid type, check error surfaces
```

#### 0b. Build polling speedup

File: `internal/ops/progress.go`

Change: `initialInterval: 1s`, `stepUpInterval: 5s`, `stepUpAfter: 30s`.

Test: Existing `progress_test.go` covers interval logic. Verify tests pass.

Risk: NEGLIGIBLE.

Build+Deploy+Test:
```
go test ./internal/ops/... -run TestPoll -v
go build -o bin/zcp ./cmd/zcp
# Deploy to zcpx, trigger a build, observe faster polling
```

#### 0c. Stage validation warning

File: `internal/ops/deploy_validate.go`

Change: Add check for `start: zsc noop --silent` on non-dev hostnames. Return warning (not error).

Test: `internal/ops/deploy_validate_test.go` -- add case.

Risk: LOW.

#### 0d. H10: KnowledgeTracker per-type tracking

Files: `internal/ops/knowledge_tracker.go`, `internal/tools/workflow_bootstrap.go`

Change:
- Add `RuntimeBriefings map[string]bool` to tracker
- `RecordBriefing(runtime, services)` populates it
- `IsLoadedFor(runtimeType string) bool` checks specific type
- `IsLoaded() bool` remains for backward compat (returns true if any briefing + scope loaded)
- Update `injectKnowledgeHint` to use per-type check (will be reworked in Cluster 2 anyway, but this is the minimal safe change)

Test: `internal/ops/knowledge_tracker_test.go` -- add per-type cases.

Risk: LOW.

Build+Deploy+Test:
```
go test ./internal/ops/... -run TestKnowledgeTracker -v
go test ./internal/tools/... -run TestWorkflow -v
go build -o bin/zcp ./cmd/zcp
# Deploy, start bootstrap, load knowledge for one runtime, check hint says "loaded for X"
```

### Phase 1: Verify Improvements (1 session)

**Deploy + test after completion.**

#### 1a. Verify internal speedup (F+G)

Files: `internal/ops/verify.go`, `internal/ops/verify_checks.go`

Changes:
1. Replace `checkHTTPHealth(url+"/health")` with `checkHTTPRoot(url+"/")` -- GET / instead of GET /health
2. Merge `checkErrorLogs(5m)` + `checkErrorLogs2m()` into single fetch -- fetch 5m, filter locally for 2m window
3. Parallelize: run log checks and HTTP checks concurrently using `errgroup`
4. Skip `startup_detected` for implicit webserver runtimes (new `hasImplicitWebServer()` already exists in `deploy_validate.go` -- reuse or export)
5. Skip `http_status` for static/nginx runtimes

Tests: `internal/ops/verify_test.go` (727 lines) -- update check names (`http_health` -> `http_root`), add cases for static/nginx runtime classification.

Risk: LOW-MEDIUM. The verify function is well-tested. Main risk: check name change (`http_health` -> `http_root`) breaks any external consumer that parses check names. Mitigation: search for `http_health` string references outside ops package.

```
grep -r "http_health" internal/ --include="*.go" | grep -v _test.go | grep -v verify
```

If no external references, safe to rename.

Build+Deploy+Test:
```
go test ./internal/ops/... -run TestVerify -v
go build -o bin/zcp ./cmd/zcp
# Deploy, run zerops_verify on a running service, check new check names appear
# Verify static/nginx service skips http_status
# Time verify: should be ~7-10s instead of 15-20s
```

#### 1b. Batch verify (`VerifyAll`)

Files: `internal/ops/verify.go` (add `VerifyAll`), `internal/tools/verify.go` (optional hostname)

Changes:
1. Add `VerifyAll()` function -- `ListServices` once, run `Verify` per service in parallel (errgroup, max 5 concurrent)
2. Make `serviceHostname` optional in `zerops_verify` tool -- when omitted, call `VerifyAll`
3. Add `VerifyAllResult` type

Tests: `internal/ops/verify_test.go` -- add `TestVerifyAll` cases. `internal/tools/verify_test.go` -- add case for missing hostname.

Risk: LOW. Single-service path unchanged. New batch path is additive.

Build+Deploy+Test:
```
go test ./internal/ops/... -run TestVerify -v
go test ./internal/tools/... -run TestVerify -v
go build -o bin/zcp ./cmd/zcp
# Deploy, call zerops_verify with no hostname, verify all services checked in parallel
# Time: should be ~7-10s for 5 services instead of 75-100s
```

### Phase 2: Type System Change (Cluster 1) (1-2 sessions)

**This is the highest-risk change. Must be atomic. Deploy + test extensively after.**

#### 2a. RED: Write failing tests first

Files: `internal/workflow/validate_test.go`, `internal/workflow/engine_test.go`, `internal/workflow/bootstrap_test.go`, `internal/tools/workflow_test.go`

Action: Write new table-driven tests for `BootstrapTarget` / `ValidateBootstrapTargets`. Do NOT delete old tests yet. New tests will fail (types don't exist).

Key test scenarios:
- Single target + CREATE dependencies
- Single target + EXISTS dependencies (requires mock live services)
- Multi-target with SHARED resolution
- IsExisting target
- Simple mode (no stage)
- Hostname overflow on stage derivation (H7)
- Storage exclusion from env var checks (H9)
- Invalid: CREATE dependency that already exists
- Invalid: EXISTS dependency that doesn't exist

#### 2b. GREEN: Implement types + validation

Files to change (all at once):
1. `internal/workflow/validate.go` -- Add `BootstrapTarget`, `RuntimeTarget`, `Dependency`, `StageHostname()`, `ValidateBootstrapTargets()`. Remove `PlannedService`, `ValidateServicePlan()`.
2. `internal/workflow/bootstrap.go` -- Change `ServicePlan.Services` from `[]PlannedService` to `Targets []BootstrapTarget`. Update `validateConditionalSkip` to use target/dependency model. Update `StepContext`.
3. `internal/workflow/engine.go` -- Change `BootstrapCompletePlan` to accept `[]BootstrapTarget`. Rename to `BootstrapCompleteDiscover` (foreshadowing step rename but safe: only called from one place).
4. `tools/workflow.go` -- Change `WorkflowInput.Plan` from `[]workflow.PlannedService` to `[]workflow.BootstrapTarget`. Update jsonschema tag.
5. `tools/workflow_bootstrap.go` -- Update routing: `input.Step == "plan"` stays for now (step names haven't changed yet). Call `BootstrapCompleteDiscover` instead of `BootstrapCompletePlan`.

Delete: `PlannedService` type entirely.

Update all existing tests that construct `PlannedService` to construct `BootstrapTarget`.

Rollback: `git revert` one commit.

Risk: HIGH. If any reference is missed, compile fails. Mitigation: `go build ./...` catches all missing references at compile time. If it compiles and tests pass, it works.

Build+Deploy+Test:
```
go test ./internal/workflow/... -count=1 -v
go test ./internal/tools/... -count=1 -v
go test ./integration/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
go build -o bin/zcp ./cmd/zcp
# Deploy to zcpx
# Test: start bootstrap, submit plan with new target format, verify acceptance
# Test: submit plan with CREATE dep that exists, verify rejection
# Test: submit multi-target plan with SHARED deps, verify dedup
```

### Phase 3: Step Consolidation (Cluster 2) (1-2 sessions)

**Depends on Phase 2. Must be atomic.**

#### 3a. RED: Write tests for 5-step model

New test cases in `bootstrap_test.go` and `engine_test.go` for:
- 5 steps: discover, provision, generate, deploy, verify
- Step categories: discover=creative, provision=fixed, generate=creative, deploy=branching, verify=fixed
- Skip logic: only generate + deploy skippable
- Step constants: `stepDiscoverEnvs` and friends replaced

#### 3b. GREEN: Implement

Files to change:
1. `workflow/bootstrap_steps.go` -- Replace 11 `stepDetails` entries with 5. Full rewrite of this file.
2. `workflow/bootstrap.go` -- Update step constants. Update `validateConditionalSkip`.
3. `workflow/bootstrap_evidence.go` -- Update `bootstrapEvidenceMap` to 5-step names.
4. `tools/workflow_bootstrap.go` -- Update routing: `input.Step == "plan"` -> `input.Step == "discover"`. Remove `injectKnowledgeHint` (absorbed into discover step guidance).
5. `content/workflows/bootstrap.md` -- Update section tags from 11-step to 5-step.
6. `workflow/bootstrap_guidance.go` -- Verify `extractSection()` still matches.
7. `integration/bootstrap_conductor_test.go` -- Update step names.
8. `integration/bootstrap_realistic_test.go` -- Update step names.

Rollback: `git revert` one commit.

Risk: HIGH. Every test that references step names breaks. Every LLM interaction using old step names breaks. Mitigation: comprehensive test update + live testing.

Build+Deploy+Test:
```
go test ./internal/workflow/... -count=1 -v
go test ./internal/tools/... -count=1 -v
go test ./integration/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
go build -o bin/zcp ./cmd/zcp
# Deploy to zcpx
# Test: full bootstrap flow with 5 steps end-to-end
# Test: skip generate (should work), skip discover (should fail)
# Test: verify step count in bootstrap response is 5
```

### Phase 4: Hard Checks (Cluster 3) (1-2 sessions)

**Depends on Phase 3. Medium risk.**

#### 4a. Context parameter (H1)

File: `workflow/engine.go`

Change: `BootstrapComplete(stepName, attestation string)` -> `BootstrapComplete(ctx context.Context, stepName string, checker StepChecker)`

This changes the function signature. Callers:
1. `tools/workflow_bootstrap.go:43` -- already has `ctx` available
2. All tests in `engine_test.go` that call `BootstrapComplete`

#### 4b. Hard check types + implementations

New files:
1. `workflow/bootstrap_checks.go` (~150 lines) -- `StepCheckResult`, `StepCheck`, `StepChecker` type, add `CheckResult` field to `BootstrapResponse`
2. `tools/workflow_checks.go` (~80 lines) -- `buildStepChecker()` factory

#### 4c. LogFetcher wiring

Files:
1. `tools/workflow.go` -- Add `logFetcher platform.LogFetcher` parameter to `RegisterWorkflow`
2. `server/server.go` -- Pass `s.logFetcher` to `RegisterWorkflow`

#### 4d. Auto-completion for mechanical steps

Modify `BootstrapComplete` in `engine.go`:
- When checker passes on provision/verify steps, auto-advance without requiring explicit `action="complete"` call
- When checker fails, return structured failure (not error)

#### 4e. Simplify `autoCompleteBootstrap`

- Remove synthetic evidence generation (gates bypassed for bootstrap)
- Keep phase transition logic (INIT -> DONE)

Build+Deploy+Test:
```
go test ./internal/workflow/... -count=1 -v
go test ./internal/tools/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
go build -o bin/zcp ./cmd/zcp
# Deploy to zcpx
# Test: provision step auto-completes when all services exist
# Test: generate step with bad zerops.yml fails hard check
# Test: verify step auto-completes when all healthy
```

### Phase 5: Outputs + Content (multiple independent sessions)

**All independent. Low risk. Deploy and test individually.**

#### 5a. Per-service decision metadata (K)

New file: `workflow/service_meta.go` (~40 lines)

Change: Called from bootstrap completion path. Pure write, no reader yet. Harmless if broken (file not written = status quo for next session).

#### 5b. CLAUDE.md reflog (I)

New file: `workflow/reflog.go` (~50 lines)

Change: Called from bootstrap completion path. Appends to CLAUDE.md. Pure write. Harmless if broken.

#### 5c. BuildInstructions routing fix (H2)

File: `server/instructions.go`

Change: When project state is CONFORMANT, check if user intent includes a runtime type not present in existing services. If so, route to bootstrap instead of deploy.

Test: `server/instructions_test.go` or appropriate test file.

#### 5d. Content deduplication (J)

File: `content/workflows/bootstrap.md`

Change: Extract repeated content (hostname rules, /status spec, dev vs stage matrix) into reference appendix sections. Update section tags.

#### 5e. Clarification guidance in discover step (L)

File: `content/workflows/bootstrap.md`

Change: Add clarification sub-section to discover guidance.

#### 5f. Mode-aware generate guidance (M)

File: `content/workflows/bootstrap.md`

Change: Filter generate guidance by plan mode (standard vs simple).

#### 5g. Env var reference validation (C2)

Files: `workflow/bootstrap.go` (add `DiscoveredEnvVars` to `BootstrapState`), `ops/deploy_validate.go` or new `workflow/bootstrap_checks.go`

Change: After provision discovers env vars, store var names. In generate hard check, validate `${hostname_varName}` patterns against stored names.

Depends on: Phase 4 (hard checks exist). Can be added as an additional check.

### Phase 6: Session Registry (H4) (optional, defer)

File: New `workflow/registry.go` (~120 lines)

This is the most complex new subsystem. It introduces file locking, PID-based stale detection, and multi-session management. The current singleton model works for the common case (single developer, single session).

**Recommendation: Defer until concurrent session issues are actually observed.** The registry adds complexity with limited immediate value. Current risk (stale state file) is managed by `action="reset"`.

If implemented, do it as a standalone phase with its own test suite, independent of all other changes.

---

## Build+Deploy+Test Cycle

After each phase:

```bash
# 1. Local verification
go test ./... -count=1 -short          # All tests pass
make lint-fast                          # No lint issues
go build -o bin/zcp ./cmd/zcp          # Clean build

# 2. Deploy to zcpx
# (deployment method depends on current setup -- SSH push or similar)

# 3. Live testing on Zerops
# - Start a new bootstrap session
# - Walk through the flow affected by the phase's changes
# - Verify MCP responses have expected structure
# - Check error paths (submit bad data, verify rejection)
```

### Live Test Scenarios by Phase

| Phase | Test Scenario | Expected Result |
|-------|--------------|-----------------|
| 0a | Import with invalid service type | Error surfaced in response (not silently dropped) |
| 0b | Trigger build deploy | Faster poll intervals visible in logs |
| 1a | `zerops_verify` on PHP service | `startup_detected` skipped, no `/health` check |
| 1b | `zerops_verify` with no hostname | All services verified in parallel |
| 2 | Submit `plan` with `{runtime, dependencies}` | Plan accepted, validated |
| 3 | Start bootstrap, complete 5 steps | Full flow works end-to-end |
| 4 | Provision step with all services created | Auto-completes without explicit call |
| 5b | Complete bootstrap fully | Reflog entry appended to CLAUDE.md |

---

## Summary: Order Comparison

### Analysis section 12 order (dependency-first):
```
1. Types -> 2. Lifecycle -> 3. Verify+Poll -> 4. Validation -> 5. Batch Verify
-> 6. Hard Checks -> 7. Step Consolidation -> 8-15. Fixes/Outputs
```

### Pragmatist order (risk-adjusted value delivery):
```
Phase 0: Independent fixes (C5, polling, stage validation, H10) -- LOW RISK, IMMEDIATE VALUE
Phase 1: Verify improvements (speedup + batch) -- LOW RISK, HIGH VALUE
Phase 2: Type system change -- HIGH RISK, FOUNDATIONAL (must happen before 3/4)
Phase 3: Step consolidation -- HIGH RISK, CORE CHANGE
Phase 4: Hard checks -- MEDIUM RISK, DEPENDS ON 2+3
Phase 5: Outputs + content -- LOW RISK, POLISH
Phase 6: Session registry -- DEFER
```

### Key differences:
1. **Independent fixes first** (Phase 0): The analysis puts them at positions 8-12. I put them first because they deliver value immediately with zero risk to the core flow. Each can be deployed and tested independently. If the core revision stalls, these improvements still ship.

2. **Verify improvements before type change** (Phase 1 before 2): The analysis interleaves verify speedup with the type change. I separate them because verify improvements are independently valuable and low-risk. They also serve as a warm-up for the codebase before tackling the high-risk changes.

3. **Type change before step consolidation** (Phase 2 before 3): Both the analysis and I agree on this order. Types must exist before steps can reference them. But I make the boundary explicit: Phase 2 is one commit, Phase 3 is another.

4. **Session registry deferred** (Phase 6): The analysis includes H4 in the core plan. I defer it. The singleton state file works fine for 95% of use cases. Registry adds complexity (file locking, PID detection) that is not needed until concurrent sessions become a real problem. Ship the core revision first.

5. **Env var validation (C2) moved to Phase 5**: The analysis puts it in Phase 4 (validation). I move it to Phase 5 because it depends on hard checks existing (Phase 4) and on discovered env vars being stored in `BootstrapState` (Phase 2 type change). It is a hard check implementation detail, not a prerequisite.

---

## Risk Mitigation Strategies

### For Phase 2 (Type System Change):
- Write ALL new tests first (RED), commit them as failing
- Implement in a separate session against committed tests (GREEN)
- Run `go build ./...` frequently -- compiler catches missed references
- Search for `PlannedService` globally before declaring done
- Deploy to zcpx and test a full bootstrap before moving to Phase 3

### For Phase 3 (Step Consolidation):
- Keep the old `bootstrap_steps.go` in git history (don't squash)
- Integration tests are the canary -- if they pass, the model works
- Test step skip logic explicitly: try skipping discover (must fail), try skipping generate (must succeed)
- Verify `extractSection()` matches new section tags in bootstrap.md

### For Phase 4 (Hard Checks):
- Start with `nil` checkers everywhere, then add one at a time
- Each checker can be tested independently against mocks
- Auto-completion can be feature-flagged (return result but don't auto-advance) during initial testing

### General:
- Never deploy to zcpx without all tests passing locally
- After each zcpx deploy, run a full bootstrap manually to verify
- Keep each phase in its own commit(s) for clean revert capability
