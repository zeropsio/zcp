# Analysis: E2E Test Quality, Redundancy & False Positives
**Date**: 2026-03-26
**Task**: Analyze all E2E tests to identify redundant, broken, or false-positive tests. Propose reliable E2E test strategy with real value. Deploy tests should be verified on zcpx where ZCP and Claude are installed. SSH access available to all services.
**Reference files**:
- `e2e/helpers_test.go` — shared E2E harness, cleanup, session helpers (254 lines)
- `e2e/bootstrap_helpers_test.go` — bootstrap-specific assertion helpers (273 lines)
- `e2e/process_helpers_test.go` — process polling helpers (148 lines)
- `e2e/bootstrap_workflow_test.go` — fresh + incremental bootstrap flows (416 lines)
- `e2e/bootstrap_modes_test.go` — simple/dev/standard mode tests (251 lines)
- `e2e/bootstrap_advanced_test.go` — multi-dep, expansion, multi-target (231 lines)
- `e2e/bootstrap_negative_test.go` — provision checker failure scenarios (176 lines)
- `e2e/deploy_test.go` — basic deploy lifecycle via zerops_deploy (293 lines)
- `e2e/deploy_git_persistence_test.go` — .git survival across deploys (360 lines)
- `e2e/deploy_prepare_fail_test.go` — PREPARING_RUNTIME_FAILED detection (235 lines)
- `e2e/deploy_error_classification_test.go` — error classification accuracy (287 lines)
- `e2e/build_logs_test.go` — BUILD_FAILED with build log capture (241 lines)
- `e2e/discover_subdomain_test.go` — multi-runtime subdomain URL + HTTP verify (437 lines)
- `e2e/subdomain_test.go` — subdomain enable/deploy/HTTP lifecycle (278 lines)
- `e2e/subdomain_lifecycle_test.go` — subdomain platform behavior deep dive (384 lines)
- `e2e/lifecycle_test.go` — full lifecycle: import/discover/env/manage/scale/events/delete (300 lines)
- `e2e/verify_test.go` — zerops_verify health checks (140 lines)
- `e2e/events_test.go` — zerops_events structure/filtering (121 lines)
- `e2e/process_test.go` — zerops_process status/cancel/errors (165 lines)
- `e2e/knowledge_quality_test.go` — knowledge claims vs live API (504 lines)
- `e2e/scale_thresholds_test.go` — MinFreeResource scaling params (286 lines)
- `e2e/scaling_debug_test.go` — SDK autoscaling data workaround (101 lines)
- `e2e/log_search_test.go` — log search timeout fix (68 lines)
- `e2e/mount_test.go` — SSHFS mount/unmount lifecycle (207 lines)
- `e2e/update_test.go` — async auto-update binary replacement (271 lines)
- `e2e/laravel_deploy_test.go` — Laravel full stack deploy (81 lines)
- `e2e/laravel_recipe_test.go` — Laravel recipe env var claims (305 lines)
- `e2e/import_zeropsyaml_test.go` — zeropsYaml import normalization (365 lines)

## Test Categories

### 1. Bootstrap Workflow (4 test files, ~1074 lines)
Tests workflow orchestration: step transitions, provision checking, mode routing.
- Fresh bootstrap, incremental, modes (simple/dev/standard), multi-dep, multi-target, expansion
- Negative: missing deps, wrong status

### 2. Deploy (5 test files, ~1416 lines)
Tests deploy via SSH (zcli push), error classification, build logs on failure.
- Basic deploy, git persistence, prepare fail, error classification, build logs

### 3. Subdomain (3 test files, ~1099 lines)
Tests subdomain enable/disable, URL format, HTTP reachability, persistence across redeploy.

### 4. Tool Verification (4 test files, ~726 lines)
Tests individual MCP tools: lifecycle, verify, events, process.

### 5. Knowledge/Platform (2 test files, ~790 lines)
Tests knowledge quality claims vs live API, scaling parameters.

### 6. Laravel-Specific (2 test files, ~386 lines)
Tests Laravel recipe env vars, full stack deploy.

### 7. Miscellaneous (3 test files, ~546 lines)
Log search, mount, auto-update.

### 8. Import (1 test file, ~365 lines)
Tests zeropsYaml normalization bug identification.
