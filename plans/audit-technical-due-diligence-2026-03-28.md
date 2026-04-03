# ZCP Technical Due Diligence Audit

**Date**: 2026-03-28
**Scope**: Full codebase (~63K LOC, 121 source + 137 test files, Go 1.25)
**Method**: 14-agent structured audit (8 comprehension + 6 cross-cutting)
**Quality Bar**: Pre-acquisition senior Go engineer due diligence

---

## Executive Summary

ZCP is a well-structured Go codebase with strong architectural foundations: clean interface design, atomic file operations, comprehensive table-driven tests (~35K test LOC), and proper concurrency patterns. The codebase demonstrates disciplined engineering with consistent conventions.

**Critical issues** center on credential handling (API token leakage in 3 error paths) and one path traversal vulnerability. **High-priority** items include ~450 lines of dead code, spec drift in 3 locations, and parameter explosion in workflow handlers. The architecture is fundamentally sound — issues are localized and fixable.

**Overall assessment**: Production-ready with targeted remediation needed on security findings.

---

## CRITICAL (fix before next release)

### C1. API Token Leaked in SSH Deploy Error Diagnostic
- **Files**: `internal/ops/deploy_ssh.go:154-185`, `internal/ops/deploy_classify.go:48`
- **Issue**: `buildSSHCommand()` embeds `authInfo.Token` in the SSH command string (`zcli login {TOKEN} && ...`). On failure, `classifySSHError` stores raw output in `PlatformError.Diagnostic`, which flows through `convertError` into the MCP JSON response. If zcli echoes the token on error, it is exposed to the LLM conversation.
- **Fix**: Add `output = bytes.ReplaceAll(output, []byte(authInfo.Token), []byte("[REDACTED]"))` immediately after `ExecSSH` returns (before any processing). Apply in both `deploy_ssh.go:121` and `deploy_classify.go:44`.
- **Found by**: 2B, 2F (cross-validated)

### C2. API Token Visible in Process Table
- **Files**: `internal/ops/deploy_local.go:104`, `internal/ops/deploy_ssh.go:158`
- **Issue**: `runner.Run(ctx, "zcli", "login", authInfo.Token)` passes the token as a command-line argument, visible via `ps aux` or `/proc/pid/cmdline` on shared containers.
- **Fix**: Pipe token via stdin (`cmd.Stdin = strings.NewReader(token)`) instead of command argument.
- **Found by**: 2F

### C3. Path Traversal via Session ID
- **Files**: `internal/workflow/session.go:162-164`
- **Issue**: MCP-supplied `sessionId` flows to `filepath.Join(stateDir, "sessions", sessionID+".json")` without validation. With `sessionID = "../../../tmp/evil"`, attacker can read/write/delete arbitrary `.json` files.
- **Fix**: Validate `sessionID` against `^[a-f0-9]{16}$` (matching `generateSessionID` output) before any path construction.
- **Found by**: 2F

### C4. Arbitrary File Read via Import filePath
- **Files**: `internal/ops/import.go:243-260`
- **Issue**: MCP `filePath` parameter passed directly to `os.ReadFile` with no path validation. LLM agent could read `/etc/passwd`, `~/.ssh/id_rsa`, etc.
- **Fix**: Restrict `filePath` to working directory subtree, or remove parameter (inline `content` already provides the capability).
- **Found by**: 2F

---

## HIGH (fix this sprint)

### H1. Deploy Guidance Omits Subdomain Enable Steps (Spec Violation)
- **Files**: `internal/workflow/deploy_guidance.go:218-243`
- **Spec**: `docs/spec-bootstrap-deploy.md:393-432` requires `zerops_subdomain action=enable` in all 3 mode workflows
- **Issue**: `writeStandardWorkflow`, `writeDevWorkflow`, `writeSimpleWorkflow` all omit subdomain enable. Bootstrap content (`bootstrap.md`) includes it correctly.
- **Fix**: Add subdomain enable step to all three deploy mode guidance functions.
- **Found by**: 2E

### H2. Spec Deploy Step Name Stale ("deploy" → "execute")
- **Files**: `docs/spec-bootstrap-deploy.md:552,655,872,874,576`
- **Issue**: Code renamed deploy workflow step 2 from "deploy" to "execute" (per analysis plan), but spec was never updated. 5+ locations reference the old name.
- **Fix**: Update spec to use "execute" throughout.
- **Found by**: 2E

### H3. Phantom CICDState in Spec State Model
- **Files**: `docs/spec-bootstrap-deploy.md:721`
- **Issue**: Spec shows `CICD: *CICDState` in WorkflowState. No such type exists — cicd is a stateless/immediate workflow.
- **Fix**: Remove from spec.
- **Found by**: 2E

### H4. pollManageProcess Timeout Silently Discarded
- **Files**: `internal/tools/scale.go:69`, `internal/tools/env.go:38,48`, `internal/tools/delete.go:47`
- **Issue**: `pollManageProcess` returns `(process, timedOut)` but timedOut is discarded with `_` in 3 tools. LLM agents cannot distinguish timeout from success. Compare with `manage.go:55-100` which handles it correctly.
- **Fix**: Handle timedOut consistently — include warning in result.
- **Found by**: 2B

### H5. tools/ Bypasses ops/ Layer — Direct platform.Client Calls
- **Files**: `internal/tools/workflow_checks.go:41,84,188`, `workflow_checks_deploy.go:106,193,203`, `workflow_deploy.go:151`, `workflow_strategy.go:184`
- **Issue**: 8 call sites in tools/ call `client.ListServices()` and `client.GetServiceEnv()` directly, bypassing ops/ validation/normalization.
- **Fix**: Extract `ops.ListServicesMap()` and `ops.GetServiceEnvVars()` helpers.
- **Found by**: 2C

### H6. Token Leak in Local Deploy stderr
- **Files**: `internal/ops/deploy_local.go:104-111`
- **Issue**: On `zcli login` failure, stderr (potentially containing token) included in error message.
- **Fix**: Sanitize `authInfo.Token` from stderr before including in error.
- **Found by**: 2B, 2F (cross-validated)

---

## MEDIUM (schedule soon)

### M1. ~450 Lines of Dead Code
Verified dead code items (all confirmed via codebase-wide grep):

| Item | File | Lines | Type |
|------|------|-------|------|
| `ErrAuthInvalidToken` | `platform/errors.go:13` | 1 | Dead error code |
| `ErrAuthAPIError` | `platform/errors.go:15` | 1 | Dead error code |
| `ErrUnknownType` | `platform/errors.go:29` | 1 | Dead error code |
| `InitSession` | `workflow/session.go:35-72` | 37 | Superseded by InitSessionAtomic |
| `RegisterSession` | `workflow/registry.go:39-53` | 14 | Only caller is dead InitSession |
| `BootstrapStoreStrategies` | `workflow/engine.go:276-290` | 15 | Strategy tool uses ServiceMeta directly |
| `Engine.Environment()` | `workflow/engine.go:34` | 1 | No production callers |
| `Engine.SessionID()` | `workflow/engine.go:101-103` | 3 | No production callers |
| `ResolveGuidance` | `workflow/bootstrap_guidance.go:12-24` | 12 | Superseded by Progressive variant |
| `SaveSessionState` (exported) | `workflow/session.go:122-124` | 3 | Test-only wrapper |
| `local_config.go` + test | `workflow/local_config.go` | 47+43 | Entire file unused |
| `runtime_resources.go` + test | `knowledge/runtime_resources.go` | 100+72 | Entire file unused |
| `uriToPath` | `knowledge/documents.go:89-92` | 4 | Zero production callers |
| `RuntimeResourceSlugs()` | `knowledge/runtime_resources.go` | 8 | Zero callers anywhere |
| Skipped test helpers | `integration/bootstrap_realistic_test.go:168-354` | ~186 | Dead agent* functions |
| `APIHarness.Cleanup()` | `platform/apitest/harness.go:96-99` | 4 | Never called |
| `statusBuildFailed` | `tools/convert.go:19` | 1 | Defined but never referenced |

**Found by**: 2A (primary), cross-validated by 2D (tests of dead code)

### M2. KnowledgeTracker Writes to Fields Nothing Reads
- **Files**: `internal/ops/knowledge_tracker.go` (37 lines), `knowledge_tracker_test.go` (58 lines)
- **Issue**: `RecordBriefing` and `RecordScope` write to unexported fields that are never read by any code. The tracker accumulates data that nothing consumes.
- **Fix**: Delete entire file + test, remove from `server.go:88` and `tools/knowledge.go:50`.
- **Found by**: 2A, 2D (cross-validated — tests are tautological with zero assertions)

### M3. Parameter Explosion in Workflow Handlers
- **Files**: `internal/tools/workflow.go:84`, `workflow_bootstrap.go:32`
- **Issue**: `handleWorkflowAction` and `handleBootstrapComplete` accept 11 parameters each. 8+ other workflow functions take 6-8 params.
- **Fix**: Introduce `WorkflowContext` struct holding {ctx, projectID, engine, client, cache, logFetcher, stateDir, selfHostname}.
- **Found by**: 2C

### M4. Magic Strings — Step Names, Actions, Workflows
- **Issue**: Step name constants duplicated in 3 locations (`bootstrap_steps.go`, `bootstrap.go:139-141`, `tools/workflow_checks.go:15-17`). Action names ("start", "complete", etc.) used as bare literals in 10-case switch. Workflow names "debug"/"configure"/"cicd" have no constants (only "bootstrap"/"deploy" do).
- **Fix**: Use exported constants from `bootstrap_steps.go` everywhere. Add `ActionStart`, `ActionComplete`, etc. constants. Add `WorkflowDebug`, `WorkflowConfigure`, `WorkflowCICD` constants.
- **Found by**: 2C

### M5. Status Normalization Logic Duplicated 3x
- **Files**: `platform/zerops_mappers.go`, `zerops_event_mappers.go`, `mock_methods.go`
- **Issue**: DONE→FINISHED, CANCELLED→CANCELED normalization switch duplicated in `mapProcess`, `mapEsProcessEvent`, and `mapEsAppVersionEvent`. FailReason extraction also duplicated.
- **Fix**: Extract `normalizeStatus(status string) string` shared helper.
- **Found by**: 1A (inventory), 2A (duplicate logic)

### M6. Network Error Detection Duplicated
- **Files**: `platform/errors.go:97-117` (`MapNetworkError`), `platform/zerops_errors.go:25-32` (inline detection)
- **Issue**: `mapSDKError` does its own inline network error detection instead of calling `MapNetworkError`.
- **Fix**: Have `mapSDKError` call `MapNetworkError`.
- **Found by**: 1A (inventory), 2A (duplicate logic)

### M7. Subdomain URL Construction Duplicated
- **Files**: `internal/ops/verify_checks.go:206-220` (`resolveSubdomainURL`), `internal/ops/subdomain.go:85-130` (`attachSubdomainUrlsToResult`)
- **Issue**: Both implement the same bare-prefix fallback pattern with `BuildSubdomainURL` + `ExtractDomainFromEnv`.
- **Fix**: Extract shared helper.
- **Found by**: 1B (inventory)

### M8. zerops_discover Uses "service" While 8 Other Tools Use "serviceHostname"
- **Files**: `internal/tools/discover.go:13` vs all other tools
- **Issue**: Inconsistent parameter naming across MCP tool suite.
- **Fix**: Rename to `serviceHostname` for consistency (API-facing change).
- **Found by**: 2E

### M9. Session State Has No Version Migration
- **Files**: `internal/workflow/state.go:16-28`, `session.go:80`
- **Issue**: `stateVersion = "1"` exists but is never checked on read. Schema changes would silently produce zero-valued fields.
- **Fix**: Add version check in `LoadSessionByID`.
- **Found by**: 2C

### M10. 9 Environment Variables Undocumented
- **Vars**: `ZCP_API_KEY`, `ZCP_AUTO_UPDATE`, `ZCP_UPDATE_URL`, `ZCP_ZCLI_DATA_DIR`, `ZCP_MAX_ITERATIONS`, `ZCP_EVAL_RESULTS_DIR`, `ZCP_EVAL_WORK_DIR`, `ZCP_EVAL_MCP_CONFIG`, container vars (`serviceId`, `hostname`, `projectId`)
- **Fix**: Add env var inventory to CLAUDE.md.
- **Found by**: 2E

### M11. Missing Test Coverage for Key Error Paths
- `VerifyAll` — zero unit tests (exported, called by tools/verify.go)
- `FetchLogs` — GetProjectLog and fetcher.FetchLogs error paths untested
- `Start/Stop/Restart/Reload` — API error paths untested
- `ConnectStorage/DisconnectStorage` — zero error path tests
- `Scale` — min>max crossover boundary untested
- Max iterations boundary (10th/11th) — spec gap
- Bootstrap concurrent session exclusivity — spec gap
- **Found by**: 2D

### M12. 73 Test Names Violate Convention
- Convention: `Test{Op}_{Scenario}_{Result}`
- 73 test functions across the codebase use `TestFoo` without `_{Scenario}_{Result}` suffix.
- **Found by**: 2D

### M13. Permanently Skipped Tests (2)
- `TestIntegration_BootstrapRealistic_FullAgentFlow` — unconditional `t.Skip()` + ~186 lines of dead helper code
- `TestAPI_Resolve_FullFlow` — unconditional `t.Skip()`, empty body, references obsolete "Task 6"
- **Found by**: 2D, 2B (cross-validated)

---

## LOW (backlog)

### L1. Bare Error Returns in init.go
- 21 bare `return err` without context wrapping in `internal/init/init.go`.
- **Found by**: 2C

### L2. Global Mutable `runner` in deploy_local.go
- Unprotected package-level var for test injection. Safe in practice (STDIO is single-threaded) but fragile.
- **Found by**: 2C

### L3. Unbounded HTTP Response Bodies (3 locations)
- `update/apply.go:97` — binary download with no size limit
- `ops/verify_checks.go:170` — /status response with no limit
- `update/check.go:130` — GitHub API JSON with no limit
- **Fix**: Wrap with `io.LimitReader`.
- **Found by**: 2F

### L4. Session State Files World-Readable
- Directories created with 0o755, should be 0o700.
- **Found by**: 2F

### L5. Unused auth.Info.Region and auth.Credentials.Region Fields
- Populated but never read in production. May be needed for future multi-region.
- **Found by**: 2A

### L6. 10 eval Package Exports That Should Be Unexported
- `ParseRecipeMetadata`, `RecipeShortName`, `GenerateHostname`, `GenerateHostnames`, `BuildTaskPrompt`, `BuildFullPrompt`, `ProtectedService`, `MatchesEvalPrefix`, `IsProtectedPath`, `DefaultAPITimeout`
- **Found by**: 2A

### L7. RegisterMount Accepts Unused runtime.Info Parameter
- Declared as `_ runtime.Info`, never used. 3 call sites pass it.
- **Found by**: 2A

### L8. engine.go 1 Line Over 350-Line Convention
- 351 lines. Extract remaining deploy methods to engine_deploy.go.
- **Found by**: 2C

### L9. Tautological Tests (3)
- `TestKnowledgeTracker_RecordBriefing/RecordScope` — zero assertions
- `TestLogsTool_WithFilters` — mock ignores all filter params
- **Found by**: 2D

### L10. Over-Mocked Tests (2)
- `TestProcessTool_Cancel` and `TestProcessTool_Status` — verify mock roundtrip, not behavior
- **Found by**: 2D

### L11. Recipe Format Variance Undocumented
- 4 of 29 recipes (django, laravel, rails, symfony) use layered inline format vs dedicated H2 sections. Both valid, but spec doesn't document the variant.
- **Found by**: 2E

### L12. 23 Plan Files Not Archived
- CLAUDE.md says "completed plans → plans/archive/". Directory doesn't exist.
- **Found by**: 2B

---

## QUICK WINS (< 1 hour each)

| # | Change | File | Effort |
|---|--------|------|--------|
| Q1 | Delete 3 dead error codes | `platform/errors.go:13,15,29` | 5 min |
| Q2 | Delete `statusBuildFailed` from convert.go | `tools/convert.go:19` | 2 min |
| Q3 | Delete `local_config.go` + test | `workflow/local_config.go`, `local_config_test.go` | 5 min |
| Q4 | Delete `runtime_resources.go` + test | `knowledge/runtime_resources.go`, `runtime_resources_test.go` | 5 min |
| Q5 | Delete `uriToPath` | `knowledge/documents.go:89-92` | 5 min |
| Q6 | Delete `APIHarness.Cleanup()` | `platform/apitest/harness.go:96-99` | 2 min |
| Q7 | Delete permanently skipped test + helpers | `integration/bootstrap_realistic_test.go:151-354` | 10 min |
| Q8 | Delete `auth_api_test.go` | `internal/auth/auth_api_test.go` | 2 min |
| Q9 | Use `workflow.StepDiscover` constant | `tools/workflow_bootstrap.go:41` | 2 min |
| Q10 | Delete duplicate step constants | `bootstrap.go:139-141`, `workflow_checks.go:15-17` | 10 min |
| Q11 | Add subdomain error code constants | `platform/errors.go` + `zerops_errors.go:78,81` | 10 min |
| Q12 | Delete unused `_ runtime.Info` param | `tools/mount.go:19` + 3 callers | 10 min |
| Q13 | Unexport `DefaultAPITimeout` | `platform/types.go:5-6` | 2 min |
| Q14 | Add session ID validation | `workflow/session.go` (3 functions) | 15 min |
| Q15 | Add token sanitization | `ops/deploy_ssh.go:121` | 15 min |

---

## TECHNICAL DEBT MAP

| Package | Debt Level | Key Issues | Priority |
|---------|-----------|------------|----------|
| `platform/` | low | 3 dead error codes, duplicated status normalization, MapNetworkError unused by mapSDKError | P2 |
| `auth/` | clean | Dead Region fields (may be intentional), 1 skipped test | P3 |
| `ops/` | low | KnowledgeTracker dead, subdomain URL duplication, FormatEnvFile orphaned, token in error paths | P1 (security) |
| `tools/` (core) | low | 1 dead constant, statusBuildFailed unused, 1 unused param | P3 |
| `tools/` (workflow) | medium | Layer violations (8 direct platform calls), parameter explosion (11 params), magic strings | P1 |
| `workflow/` | medium | Dead code (7 functions, 2 files), no schema migration, step constant duplication | P2 |
| `knowledge/` | low | 2 dead files (runtime_resources, uriToPath), search misnamed as "BM25" | P3 |
| `content/` | clean | No issues found | — |
| `server/` | low | KeepAlive at 30s (crash analysis recommended 120s), stateDir computed twice | P2 |
| `eval/` | low | 9 unnecessarily exported functions, token in process table | P2 |
| `init/` | low | 21 bare error returns | P3 |
| `update/` | low | Unbounded HTTP response bodies | P3 |
| `catalog/` | clean | No issues found | — |
| `runtime/` | clean | No issues found | — |
| `integration/` | low | 1 permanently skipped test (~186 dead lines) | P3 |
| `e2e/` | low | 1 min() redefinition, some non-self-contained tests | P3 |
| `docs/specs` | medium | 3 spec-vs-code discrepancies (deploy step name, CICDState phantom, subdomain guidance) | P1 |

---

## RECOMMENDED CLEANUP ORDER

### Wave 1: Security (1-2 days)
1. **C1+C2+H6**: Token sanitization in all deploy error paths
2. **C3**: Session ID validation (hex-only regex)
3. **C4**: Import filePath path restriction
4. **L4**: Tighten state file permissions

*Rationale: Maximum risk reduction. All are localized changes with no cross-dependencies.*

### Wave 2: Dead Code Deletion (half day)
5. **Q1-Q8**: Pure deletions, no logic changes
6. **M1**: Delete remaining dead functions (InitSession, BootstrapStoreStrategies, etc.)
7. **M2**: Delete KnowledgeTracker

*Rationale: Quick wins that reduce surface area and improve signal-to-noise ratio.*

### Wave 3: Spec Alignment (half day)
8. **H1**: Add subdomain steps to deploy guidance
9. **H2+H3**: Update spec (deploy→execute, remove CICDState)
10. **M10**: Document environment variables

*Rationale: Prevents LLM agents from receiving incorrect guidance.*

### Wave 4: Architecture Cleanup (2-3 days)
11. **H5**: Extract ops helpers to fix layer violations
12. **M3**: Introduce WorkflowContext struct
13. **M4**: Add constants for actions, workflow names
14. **M5+M6+M7**: Consolidate duplicated logic

*Rationale: Structural improvements that compound — fix foundation before adding features.*

### Wave 5: Test Improvements (ongoing)
15. **M11**: Add missing error path tests
16. **M13**: Delete permanently skipped tests
17. **L9+L10**: Fix tautological and over-mocked tests

---

## DEPENDENCY GRAPH OF FIXES

```
Independent (parallelizable):
  C1, C2, C3, C4           — security fixes (no interdependence)
  Q1-Q13                   — pure deletions
  H1, H2, H3               — spec updates
  M10                       — documentation
  L1-L12                    — all independent

Sequential chains:
  M1 (delete InitSession) → M1 (delete RegisterSession)  — RegisterSession only called by InitSession
  M2 (delete KnowledgeTracker) → must update server.go and tools/knowledge.go
  H5 (layer violations) → M3 (WorkflowContext) — context struct should include the new ops helpers
  M4 (constants) → M12 (test naming) — some test names reference the concepts being renamed

Unlocks:
  Q1-Q8 (dead code) → reduces noise for M11 (test coverage) — fewer false positives
  C3 (session ID validation) → enables M9 (schema migration) — both touch session.go
  H5 (layer fix) + M3 (context struct) → simplifies future tool development
```

---

*Generated by 14-agent structured audit. All findings verified against the codebase via grep/read.*
