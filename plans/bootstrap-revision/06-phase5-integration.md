# Phase 5: Integration Validation

**Agent**: ECHO (integration + testing)
**Dependencies**: ALL previous phases complete
**Risk**: MEDIUM — E2E on live platform

---

## Task 1: Rewrite Integration Tests

### `integration/bootstrap_conductor_test.go`
- Update all step names from 11-step to 5-step model
- Update plan submission format (BootstrapTarget)
- Verify hard check results in responses
- Test auto-completion for provision and verify steps
- Test iteration loop with hard check failure

### `integration/bootstrap_realistic_test.go`
- Full 5-step flow with mock MCP transport
- Test all 4 scenarios (fresh, add-runtime, add-managed, multi-runtime)
- Verify lifecycle progression per target
- Verify evidence populated from hard check results
- Verify service metadata written after verify
- Verify reflog entry format

---

## Task 2: Full Test Suite

```bash
# Unit tests
go test ./internal/workflow/... -count=1 -v -race
go test ./internal/ops/... -count=1 -v -race
go test ./internal/tools/... -count=1 -v -race
go test ./internal/server/... -count=1 -v -race

# Integration tests
go test ./integration/... -count=1 -v -race

# All tests with race detector
go test ./... -count=1 -race

# Full lint
make lint-local
```

---

## Task 3: Build + Deploy

```bash
./eval/scripts/build-deploy.sh
```

---

## Task 4: Manual E2E — 4 Scenarios

### Scenario A: Fresh — Single Runtime + New Dependencies
```
User intent: "Create a PHP app with PostgreSQL"
Expected:
  - discover: plan with phpdev (php-nginx@8.4) + db (postgresql@16, CREATE)
  - provision: import creates phpdev, phpstage, db. Env vars discovered.
  - generate: zerops.yml + PHP app with GET / and GET /status
  - deploy: dev + stage deployed. Subdomain enabled.
  - verify: all services healthy. Metadata + reflog written.
```

### Scenario B: Add Runtime to Existing Infrastructure
```
Prereq: Scenario A completed (phpdev+phpstage+db exist)
User intent: "Add a Node.js API"
Expected:
  - discover: plan with apidev (nodejs@22) + db (EXISTS)
  - provision: import creates apidev, apistage only. db env vars known.
  - generate: Node.js app wired to existing db.
  - deploy: apidev + apistage deployed.
  - verify: all services healthy. New reflog entry appended.
```

### Scenario C: Add Managed Service to Existing Runtime
```
Prereq: Scenario A completed
User intent: "Add Redis caching to the PHP app"
Expected:
  - discover: plan with phpdev (IsExisting=true) + db (EXISTS) + cache (valkey@7.2, CREATE)
  - provision: import creates cache only. Cache env vars discovered.
  - generate: update phpdev zerops.yml envVariables + /status for cache.
  - deploy: redeploy phpdev + phpstage.
  - verify: all services healthy including cache connectivity.
```

### Scenario D: Multiple Runtimes from Day 1
```
User intent: "PHP frontend + Node.js API + shared DB and cache"
Expected:
  - discover: plan with 2 targets:
    1. webdev (php-nginx@8.4), deps: db (CREATE), cache (CREATE)
    2. apidev (nodejs@22), deps: db (SHARED), cache (SHARED)
  - provision: single import for all services. SHARED deps deduplicated.
  - generate: code for both targets with shared dependency env vars.
  - deploy: both targets deployed.
  - verify: all services healthy.
```

---

## Task 5: Rollback Plan

If E2E reveals critical issues:
1. Revert to pre-Phase 3 commit: `git stash` or `git checkout <pre-phase3-commit>`
2. Redeploy: `./eval/scripts/build-deploy.sh`
3. The old 11-step model resumes working immediately

---

## Success Criteria

- [ ] All unit tests pass with race detector
- [ ] All integration tests pass
- [ ] Full lint clean
- [ ] Scenario A: fresh bootstrap completes in <3min
- [ ] Scenario B: add-runtime bootstrap works correctly
- [ ] Scenario C: add-managed with IsExisting works
- [ ] Scenario D: multi-runtime bootstrap works
- [ ] Batch verify completes in <15s for 8 services
- [ ] Hard check failure returns structured error (not Go error)
- [ ] Service metadata files created in .zcp/services/
- [ ] CLAUDE.md reflog entry appended correctly
- [ ] No regression: existing deploy/debug/scale/configure workflows unaffected
