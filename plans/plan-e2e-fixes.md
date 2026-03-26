# Plan: E2E Test Fixes & Improvements

**Date**: 2026-03-26
**Based on**: analysis-e2e-test-quality.analysis-1.md (4-agent deep analysis)
**Goal**: Fix broken tests, add deploy patterns, integrate catalog, add zcpx Makefile target

---

## Phase 1: Quick Fixes (3 broken tests, ~20 min)

### 1.1 Fix `verify_test.go` — add workflow start before import

**File**: `e2e/verify_test.go`
**Problem**: Line 43 calls `zerops_import` without workflow session. `zerops_import` has `requireWorkflow` guard (`tools/import.go:29`). Test crashes with WORKFLOW_REQUIRED before reaching any verify assertions.
**Fix**: Add workflow start + reset in cleanup.

```go
// After line 21 (s := newSession(t, h.srv)):
s.callTool("zerops_workflow", map[string]any{"action": "reset"})
s.mustCallSuccess("zerops_workflow", map[string]any{
    "action":   "start",
    "workflow": "bootstrap",
    "intent":   "e2e verify test",
})

// In t.Cleanup (after line 31):
s.callTool("zerops_workflow", map[string]any{"action": "reset"})
```

**Test**: `go test ./e2e/ -tags e2e -run TestE2E_Verify -v -timeout 300s`

### 1.2 Fix `knowledge_quality_test.go` Phase3 — backtick in TrimRight

**File**: `e2e/knowledge_quality_test.go:468`
**Problem**: `strings.TrimRight(v, ",]\"'")` doesn't strip backtick. Regex `(\S+@\S+)` captures trailing backtick from markdown inline code in recipes.
**Fix**: Add backtick to trim set.

```go
// Line 468: change
v = strings.TrimRight(v, ",]\"'")
// to
v = strings.TrimRight(v, ",]\"'`")
```

**Test**: `go test ./e2e/ -tags e2e -run TestE2E_KnowledgeQuality/Phase3 -v -timeout 120s`

### 1.3 Fix `knowledge_quality_test.go` Phase1 — kafka version drift

**File**: `e2e/knowledge_quality_test.go:92-97`
**Problem**: Claims table has `kafka@3.8` but platform catalog has `kafka@3.9` (verified in `active_versions.json` generated 2026-03-24).
**Fix**: Update `documentedVersions` from `[]string{"3.8"}` to `[]string{"3.9"}`.

```go
// Line 92-97: change
{
    typePattern:        "kafka",
    normalizedName:     "Kafka",
    documentedVersions: []string{"3.8"},
// to
{
    typePattern:        "kafka",
    normalizedName:     "Kafka",
    documentedVersions: []string{"3.9"},
```

Also check `services.md` and update kafka version there if it mentions 3.8.

**Test**: `go test ./e2e/ -tags e2e -run TestE2E_KnowledgeQuality/Phase1 -v -timeout 120s`

---

## Phase 2: Deploy Test Rewrite (~2 hours)

Current `deploy_test.go` is broken — does self-deploy on fresh service without running container. Rewrite to cover both real deploy patterns.

### 2.1 Rewrite `TestE2E_Deploy` — self-deploy pattern

**File**: `e2e/deploy_test.go`
**Pattern**: Service deploys itself. Code is written to target via SSH, then `zerops_deploy targetService=X` (self-deploy).

**Flow**:
```
1. Verify SSH access to zcpx (skip guard)
2. Import nodejs@22 with startWithoutCode: true, enableSubdomainAccess: true
3. Wait for RUNNING/ACTIVE status
4. SSH write zerops.yml + server.js to target:/var/www/ (base64 encode, same pattern as build_logs_test.go)
5. zerops_deploy targetService=X (self-deploy from /var/www)
6. Assert: status=DEPLOYED, mode=ssh, buildStatus=ACTIVE
7. Assert: sourceService == targetService (confirms self-deploy)
8. Wait for RUNNING status
9. Enable subdomain → poll HTTP 200 on /health
10. Delete service
```

**Key changes vs current**:
- Add `startWithoutCode: true` to import (container must exist for SSH)
- Remove `createMinimalApp` local temp dir — write directly to target via SSH
- Remove zcli login/LookPath (deploy tool handles zcli internally)
- Add SSH skip guard (like deploy_git_persistence_test.go pattern)
- Add HTTP health check via subdomain (like subdomain_test.go:212-217)

**Assertions**:
- `deployResult.Status == "DEPLOYED"`
- `deployResult.Mode == "ssh"`
- `deployResult.SourceService == deployResult.TargetService` (self-deploy confirmed)
- `httpGetStatus(subdomainURL+"/health") == 200` (server actually responds)

### 2.2 New `TestE2E_Deploy_CrossService` — dev→stage pattern

**File**: `e2e/deploy_test.go` (add to same file)
**Pattern**: Deploy from one runtime service to another. Code is on source, pushed to target.

**Flow**:
```
1. Verify SSH access to zcpx
2. Import two nodejs@22 services: devHostname + stageHostname, both startWithoutCode: true
3. Wait for both RUNNING/ACTIVE
4. SSH write zerops.yml + server.js to source:/var/www/ (via SSH from zcpx)
   - zerops.yml setup must match TARGET hostname (not source)
5. zerops_deploy sourceService=dev targetService=stage workingDir=/var/www
6. Assert: status=DEPLOYED, sourceService=dev, targetService=stage
7. Wait for stage RUNNING
8. Enable subdomain on stage → poll HTTP 200
9. Delete both services
```

**Assertions**:
- `deployResult.Status == "DEPLOYED"`
- `deployResult.SourceService != deployResult.TargetService` (cross-service confirmed)
- `httpGetStatus(stageSubdomainURL+"/health") == 200`

### 2.3 Helper: `writeAppViaSSH`

Extract SSH file-writing pattern from build_logs_test.go / deploy_prepare_fail_test.go into shared helper:

```go
// writeAppViaSSH writes zerops.yml + server.js to a remote service via SSH.
// Uses base64 encoding to avoid shell escaping issues.
func writeAppViaSSH(t *testing.T, hostname, targetDir, zeropsYml, serverJS string) {
    t.Helper()
    zeropsB64 := base64.StdEncoding.EncodeToString([]byte(zeropsYml))
    serverB64 := base64.StdEncoding.EncodeToString([]byte(serverJS))
    cmd := fmt.Sprintf(
        "mkdir -p %s && echo %s | base64 -d > %s/zerops.yml && echo %s | base64 -d > %s/server.js",
        targetDir, zeropsB64, targetDir, serverB64, targetDir,
    )
    out, err := sshExec(t, hostname, cmd)
    if err != nil {
        t.Fatalf("write app to %s:%s: %s (%v)", hostname, targetDir, out, err)
    }
}
```

### 2.4 Helper: `deployAndVerifyHTTP`

Extract the subdomain enable + HTTP poll pattern:

```go
// deployAndVerifyHTTP enables subdomain on a service and polls for HTTP 200.
func deployAndVerifyHTTP(t *testing.T, s *e2eSession, hostname string) {
    t.Helper()
    enableText := s.mustCallSuccess("zerops_subdomain", map[string]any{
        "serviceHostname": hostname,
        "action":          "enable",
    })
    var result struct {
        SubdomainUrls []string `json:"subdomainUrls"`
    }
    if err := json.Unmarshal([]byte(enableText), &result); err != nil || len(result.SubdomainUrls) == 0 {
        t.Fatalf("%s: no subdomain URL from enable", hostname)
    }
    code, ok := pollHTTPHealth(result.SubdomainUrls[0]+"/health", 5*time.Second, 90*time.Second)
    if !ok {
        t.Fatalf("%s: HTTP health check failed (last=%d), want 200", hostname, code)
    }
    t.Logf("  %s: HTTP %d OK at %s", hostname, code, result.SubdomainUrls[0])
}
```

---

## Phase 3: Import test session isolation (~30 min)

### 3.1 Fix `import_zeropsyaml_test.go` — stale session

**File**: `e2e/import_zeropsyaml_test.go`
**Problem**: Subtests create new sessions but share workflow engine with state persisted in `e2e/.zcp/state/`. If prior test run crashed, stale session blocks.
**Fix**: Add explicit reset at the START of each subtest, not just before workflow start.

```go
// In each subtest, BEFORE creating new session:
s := newSession(t, h.srv)
s.callTool("zerops_workflow", map[string]any{"action": "reset"})
```

Already done at lines 88 and 137, but the issue is that `newSession` creates a fresh MCP session while the workflow engine shares state on disk. The reset needs to happen BEFORE starting a new workflow. Verify that `action: "reset"` clears persisted sessions even without an active session object.

**Test**: `go test ./e2e/ -tags e2e -run TestE2E_Import_ZeropsYaml -v -timeout 300s`

---

## Phase 4: Claims table ← catalog integration (~1 hour)

### 4.1 Remove hardcoded `documentedVersions` from claims table

**File**: `e2e/knowledge_quality_test.go`
**Problem**: `documentedVersions` is hardcoded and goes stale (kafka@3.8). Should be derived from `active_versions.json` which is already maintained via `make catalog-sync`.
**Approach**:

1. Embed `active_versions.json` in e2e tests (same pattern as `recipe_lint_test.go:22`):
```go
//go:embed ../internal/knowledge/testdata/active_versions.json
var activeVersionsJSON []byte
```

2. Add helper to extract versions by base type from the snapshot:
```go
// catalogVersions returns active versions for a base type from the embedded catalog.
func catalogVersions(baseType string) []string {
    var snap struct{ Versions []string `json:"versions"` }
    json.Unmarshal(activeVersionsJSON, &snap)
    prefix := baseType + "@"
    var versions []string
    for _, v := range snap.Versions {
        if strings.HasPrefix(v, prefix) {
            version := strings.TrimPrefix(v, prefix)
            versions = append(versions, version)
        }
    }
    return versions
}
```

3. Remove `documentedVersions` field from `serviceClaim` struct and all entries.

4. Change Phase1 `DocumentedVersionsActive` test to:
   - For each service type, get active versions from catalog snapshot
   - Verify services.md H2 section mentions these versions
   - Verify services.md does NOT mention versions not in the catalog

5. **Side effect**: `make catalog-sync` now automatically updates what the test validates. No more manual claims table maintenance for versions.

### 4.2 Keep ports, env vars, normalizedName hardcoded

These can't be derived from the catalog — they represent what services.md SHOULD document (ports, env var patterns, H2 section names). This is intentional and correct.

---

## Phase 5: Makefile target for E2E on zcpx (~30 min)

### 5.1 Add `make e2e-zcpx` target

**File**: `Makefile`

```makefile
##########
# E2E    #
##########
e2e-build: ## Cross-compile E2E test binary for zcpx (linux/amd64)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -tags e2e -o builds/e2e-test ./e2e/

e2e-deploy: e2e-build ## Deploy E2E binary + ZCP binary to zcpx
	@echo "==> Deploying E2E test binary to zcpx..."
	scp builds/e2e-test zcpx:/var/www/e2e-test
	ssh zcpx "chmod +x /var/www/e2e-test"
	@echo "==> E2E binary deployed"

e2e-zcpx: e2e-deploy ## Run ALL E2E tests on zcpx (includes deploy + subdomain)
	ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=60 zcpx \
		"/var/www/e2e-test -test.v -test.timeout 3600s 2>&1"

e2e-zcpx-fast: e2e-deploy ## Run fast E2E tests on zcpx (read-only, ~15s)
	ssh zcpx "/var/www/e2e-test \
		-test.run 'TestE2E_Events|TestE2E_Process|TestE2E_Scaling|TestE2E_Knowledge|TestE2E_LogSearch' \
		-test.v -test.timeout 120s 2>&1"

e2e-zcpx-deploy: e2e-deploy ## Run deploy E2E tests on zcpx (~10 min)
	ssh -o ServerAliveInterval=30 -o ServerAliveCountMax=60 zcpx \
		"/var/www/e2e-test \
		-test.run 'TestE2E_Deploy|TestE2E_BuildLogs|TestE2E_DeployPrepare' \
		-test.v -test.timeout 900s 2>&1"
```

### 5.2 Update `build-deploy.sh` to also deploy E2E binary

**File**: `eval/scripts/build-deploy.sh`
Add optional `--with-tests` flag:

```bash
# After ZCP binary deploy, if --with-tests:
if [[ "${1:-}" == "--with-tests" ]]; then
    echo "==> Building E2E test binary..."
    (cd "$PROJECT_DIR" && make e2e-build)
    echo "==> Deploying E2E test binary..."
    scp "$PROJECT_DIR/builds/e2e-test" "$REMOTE_HOST:/var/www/e2e-test"
    ssh "$REMOTE_HOST" "chmod +x /var/www/e2e-test"
    echo "==> E2E test binary deployed"
fi
```

---

## Phase 6: Cleanup existing deploy tests (~30 min)

### 6.1 Align `build_logs_test.go` and `deploy_prepare_fail_test.go`

These already work correctly (cross-service deploy from zcpx). Only change:
- Extract shared SSH file-writing to `writeAppViaSSH` helper (Phase 2.3)
- Use the helper instead of duplicated base64 encode/scp pattern

### 6.2 Add SSH skip guard to all deploy tests

Consistent pattern across all deploy tests:

```go
// At start of every deploy test:
out, err := sshExec(t, "zcpx", "echo ok")
if err != nil {
    t.Skipf("SSH to zcpx failed: %s (%v)", out, err)
}
```

Tests that already have this: `deploy_prepare_fail_test.go`, `deploy_error_classification_test.go`, `deploy_git_persistence_test.go`, `build_logs_test.go`.
Tests that need it: `deploy_test.go` (after rewrite).

---

## Implementation Order

```
Phase 1 (20 min)  → 3 quick fixes → commit → verify on zcpx
Phase 2 (2 hours) → deploy rewrite + helpers → commit → verify on zcpx
Phase 3 (30 min)  → import session fix → commit → verify
Phase 4 (1 hour)  → catalog integration → commit → verify
Phase 5 (30 min)  → Makefile targets → commit → verify
Phase 6 (30 min)  → cleanup + alignment → commit → verify
```

Total: ~5 hours

## Expected Results After All Phases

```
Before:  35/43 PASS (6 FAIL, 2 SKIP)
After:   43/43+ PASS from zcpx (0 FAIL, 0 SKIP on zcpx)
         38/43+ PASS from local (5 SKIP — deploy tests need SSH)
New:     +2 tests (cross-service deploy, HTTP verify in self-deploy)
Removed: 0 tests (no consolidation — adversarial confirmed diversity is intentional)
```

## Files Changed

| File | Change | Phase |
|------|--------|-------|
| `e2e/verify_test.go` | Add workflow start + reset | 1 |
| `e2e/knowledge_quality_test.go` | Fix backtick, kafka version, catalog integration | 1+4 |
| `e2e/deploy_test.go` | Rewrite: self-deploy + cross-service + HTTP verify | 2 |
| `e2e/helpers_test.go` | Add `writeAppViaSSH`, `deployAndVerifyHTTP` helpers | 2 |
| `e2e/import_zeropsyaml_test.go` | Session isolation fix | 3 |
| `e2e/build_logs_test.go` | Use shared helper | 6 |
| `e2e/deploy_prepare_fail_test.go` | Use shared helper | 6 |
| `e2e/deploy_error_classification_test.go` | Use shared helper | 6 |
| `Makefile` | Add e2e-build, e2e-deploy, e2e-zcpx targets | 5 |
| `eval/scripts/build-deploy.sh` | Add --with-tests flag | 5 |
| `internal/knowledge/docs/services.md` | Update kafka version if needed | 1 |
