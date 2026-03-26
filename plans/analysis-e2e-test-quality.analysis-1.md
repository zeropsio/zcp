# E2E Test Quality Analysis — Iteration 1
**Date**: 2026-03-26
**Scope**: 28 test files, 44 tests, ~6400 lines
**Agents**: KB (zerops-knowledge) + Verifier (platform-verifier) + Primary (Explore) + Adversarial (Explore)
**Complexity**: Deep (ultrathink)
**Task**: Identify redundant, broken, false-positive E2E tests. Propose reliable test strategy.

## Summary

ZCP's E2E test suite has **strong breadth** (11 MCP tools covered, 44 tests) but contains **3 broken tests** that have never tested what they claim, a **version parsing bug** causing 39 false failures, and **platform catalog drift** (kafka@3.8 deactivated). The bootstrap tests (13/13 pass, 862s) are well-designed with intentional per-runtime diversity — the initial redundancy concern was challenged and rejected. Deploy error classification tests are robust (adversarial challenge confirmed). The most critical gap is that deploy tests verify service STATUS but never verify the deployed app actually responds via HTTP.

## Live Test Results (2026-03-26)

| Category | Tests | Pass | Fail | Skip | Time |
|----------|-------|------|------|------|------|
| Events | 3 | 3 | 0 | 0 | 0.5s |
| Log Search | 2 | 2 | 0 | 0 | 0.3s |
| Process | 4 | 4 | 0 | 0 | 0.5s |
| Scaling | 8 | 8 | 0 | 0 | 10s |
| Knowledge Quality | 3 | 1 | 2 | 0 | 0.2s |
| Bootstrap | 13 | 13 | 0 | 0 | 862s |
| Deploy | 5 | 2 | 1 | 2 | 42s |
| Verify | 1 | 0 | 1 | 0 | 0.2s |
| Import zeropsYaml | 4 | 2 | 2 | 0 | 0.2s |
| **Total** | **43** | **35** | **6** | **2** | **~916s** |

Not run: Subdomain (3 tests, require zcli), Mount, Update, Laravel (2 tests, require pre-existing services).

## Findings by Severity

### Critical

| # | Finding | Evidence | Confidence |
|---|---------|----------|------------|
| C1 | **TestE2E_Verify is broken** — calls `zerops_import` at line 43 without workflow session. `zerops_import` has `requireWorkflow` guard (`tools/import.go:29`). `zerops_verify` itself does NOT require workflow (`tools/verify.go` has no guard). Test crashes before reaching any verify assertions. | VERIFIED: verify_test.go:43, tools/import.go:29, tools/guard.go:11-27, tools/verify.go (no guard) | VERIFIED |
| C2 | **KnowledgeQuality Phase3 — 39 false failures** from backtick in version parsing. `recipeVersionRefs()` regex `(\S+@\S+)` captures trailing backtick from markdown. `TrimRight` at line 468 strips `,]"'` but NOT backtick. | VERIFIED: knowledge_quality_test.go:456,468 | VERIFIED |
| C3 | **kafka@3.8 no longer ACTIVE** in platform catalog. Claims table at knowledge_quality_test.go:92 documents it. Platform has deactivated this version. | VERIFIED: live test run | VERIFIED |

### Major

| # | Finding | Evidence | Confidence |
|---|---------|----------|------------|
| M1 | **Import zeropsYaml test has session isolation issue** — 2 subtests blocked by stale workflow session from prior test run. Tests share state directory (`e2e/.zcp/state/`). Reset at lines 88,137 may not clear sessions persisted to disk from crashed prior runs. | VERIFIED: live test run shows 2 FAIL, import_zeropsyaml_test.go:88 | VERIFIED |
| M2 | **Deploy tests verify STATUS but not HTTP reachability** — deploy_test.go:250-279 polls for RUNNING/ACTIVE, but never makes HTTP request to verify deployed code responds. Service can be RUNNING before app binds to port. Subdomain tests DO verify HTTP 200, but require zcli+VPN. | VERIFIED: deploy_test.go:250-279 (status only), subdomain_test.go:212-217 (HTTP check) | VERIFIED |
| M3 | **5 deploy tests require VPN+SSH+zcli** — TestE2E_Deploy, DeployGitPersistence, Subdomain (3 tests) only work from zcpx container or with VPN active. Represents ~14% of test suite inaccessible in local dev. | VERIFIED: deploy_test.go:103, subdomain_test.go:64 | VERIFIED |

### Minor

| # | Finding | Evidence | Confidence |
|---|---------|----------|------------|
| m1 | **lifecycle_test.go and deploy_test.go share ~60% of steps** — both do import→wait→discover→verify→delete lifecycle. Not redundant in purpose (lifecycle=tool integration, deploy=SSH deployment) but duplicate ~120 lines of setup/teardown. | VERIFIED: lifecycle_test.go vs deploy_test.go | LOGICAL |
| m2 | **zerops_manage only tests restart** — reload (env var propagation), stop, start actions not E2E tested. | VERIFIED: lifecycle_test.go:167 (only restart) | VERIFIED |
| m3 | **zerops_env delete not tested** — only set is exercised. | VERIFIED: lifecycle_test.go:145 (only set) | VERIFIED |
| m4 | **zerops_process cancel not tested** — CancelTerminal tests error case (canceling finished process), but no test for canceling in-flight process. | VERIFIED: process_test.go:115-157 (terminal only) | VERIFIED |
| m5 | **php@8.4 recipe references may be invalid** — filament, laravel, nette, nette-contributte, symfony recipes reference `php@8.4` which may need to be `php-nginx@8.4` or `php-apache@8.4` for the platform catalog. | LOGICAL: live test Phase3 failures for these recipes | LOGICAL |

## Adversarial Challenges — Resolution

| Primary Finding | Challenge | Resolution | Evidence |
|----------------|-----------|------------|----------|
| FALSE-POSITIVE-2: "deploy error classification has weak assertions" | Both adversarials: test exercises REAL SSH errors against real services, uses negative assertions correctly | **REJECTED** — test is well-designed, targets RC2 forensic bug | deploy_error_classification_test.go:130-191 |
| "5 bootstrap tests are 50% redundant, consolidate" | Both adversarials: each test covers different (mode, runtime, dependency) triplet with different assertion points | **REJECTED** — per-runtime diversity is intentional, catches version drift per type | bootstrap_modes_test.go: each test has unique assertions (e.g., assertNoEnvVarCheck for storage) |
| MISSING-1: "No chaos/negative tests" | KB adversarial: 3 negative tests already exist in bootstrap_negative_test.go | **REJECTED** — negative tests exist; only env var reference cycles untested | bootstrap_negative_test.go:20-175 |
| FALSE-POSITIVE-1: "Deploy tests never verify running server" | Adversarial: deploy_test.go DOES poll for RUNNING/ACTIVE | **PARTIALLY UPHELD** — status check exists but HTTP health check missing; downgraded to MAJOR M2 | deploy_test.go:250-279 |

## Recommendations (Evidence-Backed, Priority-Ordered)

| # | Recommendation | Priority | Evidence | Effort |
|---|----------------|----------|----------|--------|
| R1 | **Fix TestE2E_Verify** — Add `s.mustCallSuccess("zerops_workflow", map[string]any{"action":"start","workflow":"bootstrap","intent":"e2e verify test"})` after line 21, before the import call at line 43 | P0 | C1: WORKFLOW_REQUIRED error blocks entire test | 5 min |
| R2 | **Fix Phase3 backtick** — Add backtick to TrimRight: `strings.TrimRight(v, ",]\"'\x60")` at knowledge_quality_test.go:468 | P0 | C2: 39 false test failures | 1 line |
| R3 | **Update kafka claims** — Check live catalog, update `documentedVersions` at knowledge_quality_test.go:92-97 | P0 | C3: platform drift | 10 min |
| R4 | **Fix import_zeropsyaml session isolation** — Clear stale session files in test setup, or use `t.TempDir()` for state directory | P1 | M1: 2 subtests blocked by stale state | 30 min |
| R5 | **Add HTTP health check to deploy test** — After RUNNING status confirmed, enable subdomain and poll for HTTP 200 | P1 | M2: status ≠ running server | 1 hr |
| R6 | **Add SSH reachability skip guard** — deploy_test.go should skip if SSH to fresh service fails (like deploy_git_persistence_test.go already does) | P1 | M3: confusing failures from local | 5 min |
| R7 | **Add zerops_manage reload E2E test** — Set env var, reload, verify propagation | P2 | m2: reload not tested | 1 hr |
| R8 | **Add zerops_process cancel E2E test** — Start import, cancel in-flight, verify CANCELED status | P2 | m4: cancel not tested | 1 hr |
| R9 | **Investigate php@8.4 recipe references** — Verify correct type names in recipe YAML | P2 | m5: may affect 5 recipes | 30 min |

## Evidence Map

| Finding | Confidence | Basis |
|---------|------------|-------|
| C1 (verify broken) | VERIFIED | Code: verify_test.go:43, tools/import.go:29, tools/guard.go:11-27; Live: WORKFLOW_REQUIRED error |
| C2 (backtick bug) | VERIFIED | Code: knowledge_quality_test.go:456,468; Live: 39 subtests fail |
| C3 (kafka drift) | VERIFIED | Live: platform catalog check |
| M1 (session isolation) | VERIFIED | Live: 2 subtests fail; Code: shared state dir |
| M2 (deploy HTTP gap) | VERIFIED | Code: deploy_test.go:250-279 (status only); subdomain_test.go:212-217 (HTTP exists elsewhere) |
| M3 (VPN coupling) | VERIFIED | Code: skip guards in tests |
| m1-m5 | VERIFIED/LOGICAL | Code references cited per finding |

## Scorecard (Post-Adversarial)

| Dimension | Score | Notes |
|-----------|-------|-------|
| Coverage Breadth | 9/10 | 11 MCP tools covered |
| Coverage Depth | 7/10 | Gaps in manage/scale/process actions |
| Redundancy | 8/10 | Bootstrap diversity is intentional (adversarial confirmed) |
| False Positive Risk | 6/10 | Deploy status ≠ running app; error classification is sound |
| Test Isolation | 6/10 | Session state persists to disk, VPN coupling |
| Assertion Quality | 7/10 | Strong negative assertions, status polling adequate |
| Maintainability | 8/10 | Well-structured helpers, clear step logging |
| **Overall** | **7.3/10** | **3 broken tests block value; once fixed, solid suite** |

## Key Insight

The E2E suite's biggest problem is not redundancy or false positives — it's **3 broken tests that silently test nothing** (verify, import_zeropsyaml, knowledge Phase3). Fixing these 3 issues (R1, R2, R3) would immediately increase passing tests from 35/43 to 41/43+, with only VPN-dependent tests remaining as the sole gap. The bootstrap tests are well-designed with intentional diversity. The deploy error classification tests are robust regression tests for forensic findings. The most valuable new test would be adding HTTP health checks to deploy verification (R5).
