# Bootstrap Flow Revision — Master Implementation Plan

> Generated from multi-agent analysis: Architect, Pragmatist, Devil's Advocate, Evaluator, Feature Decomposition.
> Source: `plans/analysis-bootstrap-revision.md` (1362 lines)

---

## Executive Summary

Replace the 11-step flat bootstrap workflow with a 5-step runtime-centric model. The core changes:

1. **BootstrapTarget replaces PlannedService** — runtime + dependencies structure instead of flat list
2. **11 steps → 5 steps** — discover, provision, generate, deploy, verify
3. **Hard checks replace LLM attestation** — real API calls validate step completion
4. **Batch verify** — parallel service verification (75s → ~15s)
5. **CLAUDE.md reflog + service metadata** — cross-session context without stale state

---

## Agent Consensus & Resolved Disagreements

### Agreed by all agents:
- BootstrapTarget is strictly better than PlannedService (topology > flat list)
- Hard checks are strictly better than attestation strings
- Verify speedup is independent and low-risk
- Import error bug (C5) is real but low-severity
- No backward compatibility needed (early development)

### Devil's Advocate concerns resolved:
| Concern | Resolution |
|---------|-----------|
| Registry model (H4) over-engineering | **DEFERRED** — add PID+TTL to existing state file instead. Full registry only if concurrent sessions become a real problem. |
| StepChecker layering violation | **ACCEPTED** — keep checks in tool layer, pass results to engine. Engine never calls platform API directly from checks. |
| SHARED resolution underspecified | **ADDRESSED** — explicit algorithm: collect all CREATE hostnames, then for each target's dependency, if hostname appears in another target's CREATE list, promote to SHARED. Error if duplicate CREATE within same target. |
| Auto-completion trigger unclear | **CLARIFIED** — provision auto-completes when `zerops_import` result triggers a check via `action="complete"`. Not implicit — LLM still calls complete, but hard check verifies instead of trusting attestation. |
| CLAUDE.md reflog token waste | **ACCEPTED risk** — bootstrap runs 2-5 times per project lifetime. Negligible token cost. |
| Decision metadata has no consumer | **ACCEPTED** — deploy workflow will consume it (future). Still worth writing as historical record. |

### Evaluator corrections applied:
| Correction | Impact |
|-----------|--------|
| PlannedService has 48 refs (not ~35) across 7 files | Plan accounts for full blast radius |
| KnowledgeTracker already has briefing history slice | H10 is simpler: add `IsLoadedForType()` method using existing `briefingCalls` data |
| Import bug is low-severity (polling catches failures) | C5 remains in plan but deprioritized |
| State file migration risk | Added note: old state files are simply deleted (no migration needed per CLAUDE.md) |

---

## Implementation Phases

### Phase 0: Independent Quick Wins (no dependencies)

**Items**: 8 (Import error fix), 11 (BuildInstructions routing), 12 (KnowledgeTracker), polling speedup from item 3

**Rationale**: These deliver value immediately with zero risk to existing behavior. All are additive or small fixes.

#### Feature 0A: Import Error Surfacing (C5)
- **Files**: `internal/ops/import.go` (add `ServiceErrors` field + collection)
- **Tests**: `internal/ops/import_test.go` (new table cases for per-service errors)
- **Complexity**: S
- **Risk**: None — additive field on `ImportResult`

#### Feature 0B: BuildInstructions Routing Fix (H2)
- **Files**: `internal/server/instructions.go` (update CONFORMANT case to suggest bootstrap for new runtime types)
- **Tests**: New/updated instructions tests
- **Complexity**: S
- **Risk**: None — system prompt text change only

#### Feature 0C: KnowledgeTracker Per-Type (H10)
- **Files**: `internal/ops/knowledge_tracker.go` (add `IsLoadedForType(string) bool` using existing `briefingCalls` slice)
- **Tests**: `internal/ops/knowledge_tracker_test.go` (per-type loading scenarios)
- **Complexity**: S
- **Risk**: None — additive method, existing `IsLoaded()` unchanged

#### Feature 0D: Build Polling Speedup
- **Files**: `internal/ops/progress.go` (initial 3s→1s, stepUp 10s→5s, stepUpAfter 60s→30s)
- **Tests**: `internal/ops/progress_test.go` (update expected intervals)
- **Complexity**: S
- **Risk**: None — faster polling with same timeout

#### Deploy & Test Cycle
```bash
go test ./internal/ops/... ./internal/server/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
./eval/scripts/build-deploy.sh
# Test: zerops_import with invalid services → verify error surfacing
# Test: zerops_knowledge → verify per-type tracking
```

---

### Phase 1: Foundation — BootstrapTarget Types (Lane A start)

**Items**: 1 (BootstrapTarget), 2 (Lifecycle)

**Rationale**: Everything else depends on the new type structure. This is the atomic foundation.

#### Feature 1A: BootstrapTarget Types + Validation
- **Files modified** (7):
  - `internal/workflow/validate.go` — FULL REWRITE: `BootstrapTarget`, `RuntimeTarget`, `Dependency`, `ValidateBootstrapTargets()`, `StageHostname()`, SHARED resolution, H7 stage overflow, H9 storage classification
  - `internal/workflow/bootstrap.go` — update `BootstrapState.Plan` usage, `validateConditionalSkip()` for new types
  - `internal/workflow/engine.go` — `BootstrapCompletePlan()` accepts `[]BootstrapTarget`, step name `"plan"` → `"discover"` (or kept until phase 3)
  - `internal/workflow/bootstrap_evidence.go` — update step names in evidence map
  - `internal/workflow/managed_types.go` — add `isManagedStorage()` for H9
  - `internal/tools/workflow.go` — `WorkflowInput.Plan` field type change
  - `internal/tools/workflow_bootstrap.go` — update routing for plan step
- **Tests** (4 files, ~510 lines rewritten):
  - `internal/workflow/validate_test.go` — FULL REWRITE against BootstrapTarget
  - `internal/workflow/engine_test.go` — update BootstrapCompletePlan tests
  - `internal/workflow/bootstrap_test.go` — update conditional skip tests
  - `internal/tools/workflow_test.go` — update plan submission tests
- **Delete**: `PlannedService` type entirely
- **Complexity**: L
- **Risk**: MEDIUM — touches many files atomically. Mitigated by: no backward compat needed, integration tests use JSON (field names preserved in spirit)

#### Feature 1B: Session-Scoped Lifecycle
- **Files modified** (2):
  - `internal/workflow/bootstrap.go` — add `DiscoveredEnvVars map[string][]string`, lifecycle constants, per-target lifecycle field
  - `internal/workflow/engine.go` — add `updateLifecycle()`, `StoreDiscoveredEnvVars()`
- **Tests**: bootstrap_test.go, engine_test.go (new lifecycle test cases)
- **Complexity**: S
- **Risk**: LOW — additive `omitempty` fields

#### Deploy & Test Cycle
```bash
go test ./internal/workflow/... -count=1 -v
go test ./internal/tools/... -count=1 -v
go test ./integration/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
./eval/scripts/build-deploy.sh
# Test: zerops_workflow action="start" workflow="bootstrap" → submit plan with new BootstrapTarget format
# Test: verify lifecycle tracking in state file
```

---

### Phase 2: Performance — Verify Speedup + Batch (Lane B, parallel with Phase 1)

**Items**: 3 (Verify speedup + runtime classification), 4 (Env ref validation + stage checks), 5 (Batch verify)

**Rationale**: All ops-layer changes. Independent from Phase 1 (different packages). Can run in parallel.

#### Feature 2A: Verify Speedup + Runtime Classification (C1)
- **Files modified** (3):
  - `internal/ops/verify.go` — replace `checkHTTPHealth` with `checkHTTPRoot` (GET /), add runtime classification, parallelize log+HTTP groups, reduce 3 log fetches to 2
  - `internal/ops/verify_checks.go` — add `checkHTTPRoot()`, `batchLogChecks()`, runtime class helpers. Remove `checkHTTPHealth()`. Skip `/status` for static/nginx. Skip `startup_detected` for implicit webserver
  - `internal/ops/deploy_validate.go` — fix `Base` field to `any` + `baseStrings()` normalizer (C4)
- **Tests** (3 files):
  - `internal/ops/verify_test.go` — runtime classification, new check names, static/worker skip
  - `internal/ops/deploy_validate_test.go` — multi-base type, stage zsc noop warning
  - `internal/tools/verify_test.go` — updated check names in response
- **Complexity**: L

#### Feature 2B: Env Ref Validation (C2)
- **Files**: `internal/ops/deploy_validate.go` — add `ValidateEnvReferences()` (hostname + varName, case-sensitive)
- **Tests**: `internal/ops/deploy_validate_test.go` — valid/invalid refs, case sensitivity
- **Complexity**: M
- **Depends on**: Feature 1B (DiscoveredEnvVars in BootstrapState)

#### Feature 2C: Batch Verify (VerifyAll)
- **Files modified** (2):
  - `internal/ops/verify.go` — add `VerifyAll()` with errgroup (max 5 concurrency), `VerifyAllResult` type
  - `internal/tools/verify.go` — make `serviceHostname` optional, call `VerifyAll()` when omitted
- **Tests**: verify_test.go, verify tool tests
- **Complexity**: M
- **Depends on**: Feature 2A (runtime classification must be done first)

#### Deploy & Test Cycle
```bash
go test ./internal/ops/... -count=1 -v
go test ./internal/tools/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
./eval/scripts/build-deploy.sh
# Test: zerops_verify (no hostname) → batch verify all services
# Test: zerops_verify serviceHostname=phpdev → single verify with new check names
# Test: verify static/nginx service → confirm /status skipped
```

---

### Phase 3: Core Value — Hard Checks + Step Consolidation

**Items**: 6 (Hard checks + auto-completion), 7 (Step consolidation 11→5)

**Depends on**: Phase 1 AND Phase 2 both complete.

**Rationale**: This is the core value delivery. Hard checks must exist before step consolidation (the safety of 11 separate steps is replaced by hard checks at 5 step boundaries).

#### Feature 3A: Hard Checks (StepChecker)
- **New files** (2):
  - `internal/workflow/bootstrap_checks.go` (~40 lines) — `StepCheckResult`, `StepCheck`, `StepChecker` types
  - `internal/tools/workflow_checks.go` (~200 lines) — `buildStepChecker()` + per-step check implementations (checkProvision, checkGenerate, checkDeploy, checkVerify)
- **Files modified** (5):
  - `internal/workflow/engine.go` — `BootstrapComplete()` gains `context.Context` + `StepChecker` (H1). Add auto-completion logic.
  - `internal/workflow/bootstrap.go` — add `CheckResult *StepCheckResult` to `BootstrapResponse`
  - `internal/tools/workflow.go` — `RegisterWorkflow` gains `logFetcher` param. `handleBootstrapComplete` builds checker.
  - `internal/tools/workflow_bootstrap.go` — pass checker through
  - `internal/server/server.go` — pass `s.logFetcher` to RegisterWorkflow
- **Tests** (3 files):
  - `internal/workflow/engine_test.go` — all `BootstrapComplete` tests gain ctx+checker. Test hard check failure returns structured response.
  - `internal/tools/workflow_test.go` — test checker construction and failure paths
  - NEW: `internal/tools/workflow_checks_test.go` (~300 lines) — per-step checker tests
- **Complexity**: XL — central piece, each checker has API calls + validation

#### Feature 3B: Step Consolidation (11 → 5)
- **Files modified** (6):
  - `internal/workflow/bootstrap_steps.go` — FULL REWRITE: 5 steps with new guidance
  - `internal/workflow/bootstrap.go` — update step constants, skip logic, `NewBootstrapState()` (5 steps)
  - `internal/workflow/bootstrap_evidence.go` — update evidence map
  - `internal/workflow/bootstrap_guidance.go` — update section tag extraction
  - `internal/content/workflows/bootstrap.md` — MAJOR REWRITE: 5 sections
  - `internal/tools/workflow_bootstrap.go` — update step name references, remove `injectKnowledgeHint` (knowledge loading is now part of discover)
- **Tests** (4 files):
  - `internal/workflow/bootstrap_test.go` — all tests use new 5-step names
  - `internal/workflow/engine_test.go` — update step names
  - `internal/workflow/bootstrap_guidance_test.go` — update section tags
  - `internal/tools/workflow_test.go` — update step names in complete/skip tests
- **Complexity**: L — many files but mostly mechanical renaming + content merge
- **MUST be atomic with Feature 3A** — deploying 5 steps without hard checks loses safety guarantees

#### Deploy & Test Cycle
```bash
go test ./internal/workflow/... -count=1 -v
go test ./internal/tools/... -count=1 -v
go test ./integration/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
./eval/scripts/build-deploy.sh
# CRITICAL E2E TEST: Full bootstrap flow with 5 steps
# Test: zerops_workflow action="start" workflow="bootstrap"
# Test: submit discover plan → hard check validates
# Test: provision auto-completes when services exist + env vars present
# Test: verify runs VerifyAll with hard checks
# Test: iterate loop on deploy failure
```

---

### Phase 4: Outputs — Metadata, Reflog, Guidance

**Items**: 9 (Service metadata), 10 (CLAUDE.md reflog), 13 (Clarification guidance), 14 (Mode-aware guidance), 15 (Content dedup)

**Depends on**: Phase 3 complete.

#### Feature 4A: Per-Service Decision Metadata
- **New files**: `internal/workflow/service_meta.go` (~50 lines), `service_meta_test.go` (~80 lines)
- **Complexity**: S

#### Feature 4B: CLAUDE.md Reflog
- **New files**: `internal/workflow/reflog.go` (~50 lines), `reflog_test.go` (~80 lines)
- **Complexity**: S

#### Feature 4C: Clarification + Mode-Aware Guidance
- **Files**: `bootstrap_steps.go`, `bootstrap.md`, `bootstrap.go` (mode filtering in BuildResponse)
- **Tests**: guidance tests, bootstrap response tests
- **Complexity**: S

#### Feature 4D: Content Deduplication
- **Files**: `bootstrap.md` — add reference appendix, remove inline duplicates
- **Tests**: guidance extraction tests
- **Complexity**: S

#### Deploy & Test Cycle
```bash
go test ./internal/workflow/... -count=1 -v
go test ./... -count=1 -short
make lint-fast
./eval/scripts/build-deploy.sh
# Test: Complete bootstrap → verify .zcp/services/ files created
# Test: Complete bootstrap → verify CLAUDE.md reflog entry appended
# Test: Simple mode plan → verify mode-filtered guidance
```

---

### Phase 5: Integration Validation

**Depends on**: All previous phases.

#### Tasks:
1. Rewrite `integration/bootstrap_conductor_test.go` for 5-step model
2. Rewrite `integration/bootstrap_realistic_test.go` for 5-step model
3. Full test suite: `go test ./... -count=1 -race`
4. Full lint: `make lint-local`
5. Build + deploy + manual E2E across all 4 scenarios:
   - **Fresh**: "PHP + PostgreSQL" → 5 steps → reflog → metadata
   - **Add runtime**: "add Node.js API" → existing deps → new runtime
   - **Add managed**: "add Valkey" → IsExisting target → redeploy
   - **Multi-runtime**: "PHP frontend + Node API + shared DB"

#### Deploy & Final Test
```bash
go test ./... -count=1 -race
make lint-local
./eval/scripts/build-deploy.sh
# Manual E2E: All 4 scenarios on live Zerops
```

---

## Agent Team Design for Implementation

### Recommended Team Structure

```
LEAD (coordinator)
  |
  +-- ALPHA (workflow specialist)     — Phases 1, 3B
  +-- BRAVO (ops specialist)          — Phase 2
  +-- CHARLIE (hard checks)           — Phase 3A
  +-- DELTA (outputs + content)       — Phases 0, 4
  +-- ECHO (integration + testing)    — Phase 5
```

### Execution Timeline

```
         Phase 0    Phase 1    Phase 2    Phase 3    Phase 4    Phase 5
ALPHA    ------     ████████   ------     ████████   ------     ------
BRAVO    ------     ------     ████████   ------     ------     ------
CHARLIE  ------     ------     ------     ████████   ------     ------
DELTA    ████████   ------     ------     ------     ████████   ------
ECHO     ------     ------     ------     ------     ------     ████████
LEAD     ░░░░░░░░   ░░░░░░░░   ░░░░░░░░   ░░░░░░░░   ░░░░░░░░   ░░░░░░░░

████ = active work
░░░░ = coordination, code review, deploy+test
```

### Phase Gate Protocol

After EACH phase:
1. All affected tests pass (`go test ./... -count=1 -short`)
2. Lint clean (`make lint-fast`)
3. Build + deploy to zcpx:
   ```bash
   ./eval/scripts/build-deploy.sh
   ```
   This builds for linux/amd64, SCPs to zcpx, verifies SHA256 hash, and kills stale `zcp serve` processes.
4. Live test on zcpx: ECHO agent runs the test scenarios for that phase
5. LEAD reviews results, approves next phase

### Agent Instructions Template

Each agent receives:
```
You are {ROLE} agent. Your responsibility: {SCOPE}.

MANDATORY: TDD discipline.
1. Write failing tests FIRST (RED)
2. Implement minimal code to pass (GREEN)
3. Refactor (tests stay green)

Current phase: {PHASE_NUMBER}
Your features: {FEATURE_LIST}
Dependencies: {COMPLETED_PHASES}

After completing your work:
1. Run: go test ./... -count=1 -short
2. Run: make lint-fast
3. Report results to LEAD

DO NOT modify files outside your scope without LEAD approval.
```

---

## Dependency Graph (Visual)

```
Phase 0 (independent)          Phase 1 (foundation)        Phase 2 (performance)
  0A: Import error fix           1A: BootstrapTarget types    2A: Verify speedup
  0B: BuildInstructions          1B: Session lifecycle        2B: Env ref validation
  0C: KnowledgeTracker                                       2C: Batch verify
  0D: Polling speedup

                    ↓                         ↓
                    └─────────┬───────────────┘
                              ↓
                    Phase 3 (core value)
                      3A: Hard checks (XL)
                      3B: Step consolidation (L)
                              ↓
                    Phase 4 (outputs)
                      4A: Service metadata
                      4B: CLAUDE.md reflog
                      4C: Guidance updates
                      4D: Content dedup
                              ↓
                    Phase 5 (integration)
                      Integration tests
                      Full E2E validation
```

---

## Risk Matrix

| Phase | Risk | Mitigation |
|-------|------|-----------|
| Phase 0 | None | Independent fixes, fully reversible |
| Phase 1 | MEDIUM — 48 refs across 7 files | Delete PlannedService outright, rewrite all tests in one pass. Single commit. |
| Phase 2 | LOW — ops-layer only | Independent from Phase 1. Each feature has its own test suite. |
| Phase 3 | HIGH — core behavioral change | Must be atomic (3A+3B together). Full integration test suite must pass. |
| Phase 4 | LOW — additive outputs | New files only. No existing behavior changed. |
| Phase 5 | MEDIUM — E2E on live platform | Manual verification required. Have rollback plan (revert to pre-Phase 3 binary). |

---

## Total Impact

| Metric | Count |
|--------|-------|
| Files modified | ~25 |
| New files | ~8 |
| Lines added | ~1800 |
| Lines removed | ~800 |
| Net change | ~+1000 |
| New test cases | ~60 |
| Test files modified | ~12 |

---

## Deferred Items (Not In This Plan)

| Item | Reason |
|------|--------|
| Session registry (H4) | Over-engineered for current single-process use. Add PID+TTL to state file if needed. |
| Destructive re-bootstrap | Explicit user confirmation required. Handled outside bootstrap. |
| Concurrent workflow sessions | Not a real problem for STDIO server. Defer until it is. |
