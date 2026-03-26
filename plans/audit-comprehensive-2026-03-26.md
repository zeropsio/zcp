# ZCP Comprehensive Code Audit — 2026-03-26

**Scope**: Every `.go` file in the repository (61K lines, 160+ files, 15 packages).
**Method**: 8 parallel audit agents + isolated verification of all major findings.
**All findings verified against actual source code with line numbers.**

---

## How to Read This

This audit separates **real problems** (things that will bite you in production or mislead agents) from noise. Findings are grouped by **systemic root cause**, not by file. Each section explains what's actually wrong, where it manifests, and why it matters in the context of ZCP's mission: an MCP server that AI agents trust with infrastructure operations.

---

## 1. CREDENTIAL EXPOSURE IN ERROR PATH

**Root cause**: The SSH deploy command embeds `authInfo.Token` as a plaintext argument. When SSH fails, the raw command output (including the token) flows through the error chain unsanitized into the MCP response.

**Trace** (verified line-by-line):
```
ops/deploy.go:184      → fmt.Sprintf("zcli login %s", authInfo.Token)
platform/deployer.go:41 → SSHExecError{Output: string(combinedOutput)}
ops/deploy_classify.go:48 → PlatformError{Diagnostic: output}  // no sanitization
tools/convert.go:43     → result["diagnostic"] = pe.Diagnostic  // sent to LLM
```

**Impact**: If `zcli login` fails on the remote host (invalid token, network issue, zcli not installed), the token appears in the `diagnostic` field of the JSON response sent to the LLM agent. The LLM may then include it in conversation context, logs, or follow-up tool calls.

**Why this wasn't caught**: No `sanitize` function exists anywhere in `internal/ops/`. The `internal/workflow/reflog.go` has a `sanitizeReflogIntent()` but it only strips newlines for markdown safety — completely unrelated. No test covers the "zcli login fails" error path.

**Systemic fix**: A single `sanitizeSSHOutput(output string, token string) string` that redacts the token from any SSH output before it enters `PlatformError.Diagnostic`. Apply at `deploy_classify.go:44` (the only funnel point).

---

## 2. MUTEX HELD DURING NETWORK I/O

**Root cause**: `platform/zerops.go:48-57` — `getClientID()` acquires `z.mu.Lock()`, then calls `z.GetUserInfo(ctx)` (HTTP request to Zerops API) while holding the lock.

**What calls `getClientID()`**: `ListServices`, `SearchProcesses`, `SearchAppVersions`, `GetProjectEnv` — the four most-used API methods.

**Impact**: If the API is slow (common during Zerops maintenance windows), every concurrent MCP tool call that needs service data blocks on a single HTTP request. The entire MCP server serializes around one slow API response.

**Why the current pattern exists**: Classic lazy-init with mutex. The cached `clientID` is used after the first call, so this only blocks on cold start. But cold start happens on every new MCP session.

**Systemic fix**: Double-check lock. Read under RLock → return if cached → upgrade to Lock → re-check → fetch → store. One-time cost, no serialization.

---

## 3. NON-DETERMINISTIC MAP ITERATION (6 locations)

**Root cause**: Go maps iterate in random order. ZCP generates LLM-facing guidance text by iterating maps, producing different output on each call for identical inputs.

**Locations** (all verified):
| File | Line | What's non-deterministic |
|------|------|--------------------------|
| `workflow/bootstrap_guide_assembly.go:46` | `formatEnvVarsForGuide` | Env var guide hostname ordering |
| `workflow/deploy_guidance.go:194` | `writeStrategyNote` | Strategy alternatives order |
| `workflow/router.go:191` | `strategyOfferings` | Dominant strategy on ties |
| `tools/workflow_strategy.go:42` | `handleStrategy` | Partial state on error (writes before reads all fail) |
| `server/instructions_orientation.go:84` | Knowledge pointer section | Service block ordering |
| `knowledge/briefing.go:148` | `detectRecipeRuntime` | Recipe-runtime matching on collision |

**Impact**: LLM agents see different guidance for the same project state. Not a correctness bug per se, but makes debugging harder and can cause agents to behave differently across sessions for no reason. The `workflow_strategy.go` case is worse — it can leave partial writes on error because it iterates + writes before confirming all reads succeed.

**Systemic fix**: One utility: `sortedKeys[K comparable](m map[K]V) []K`. Apply at all 6 locations. For `workflow_strategy.go`, additionally validate all reads before any writes.

---

## 4. CLIENT-SIDE API FILTERING

**Root cause**: `platform/zerops.go:180`, `zerops_search.go:224,268` — `ListServices`, `SearchProcesses`, and `SearchAppVersions` fetch ALL entities across ALL projects for the entire account, then filter client-side by `projectID`.

**Impact**: For accounts with many projects (common in agencies/teams), every `zerops_discover` call downloads the full service inventory. This is both slow and wastes bandwidth. The Zerops search API supports `projectId` in the filter — it's just not set.

**Systemic fix**: Add `projectId` to the search filter in `ListServices`, `SearchProcesses`, `SearchAppVersions`. This is a one-field addition to the SDK filter struct at each call site.

---

## 5. UNBOUNDED READS

Two locations read potentially large data without limits:

| File | Line | What | Risk |
|------|------|------|------|
| `ops/verify_checks.go:170` | `io.ReadAll(resp.Body)` on 2xx | A `/status` endpoint returning huge body → OOM | Moderate (attacker controls endpoint) |
| `eval/runner.go:127` | `os.ReadFile(logFile)` | Claude eval JSONL log can be hundreds of MB → OOM | Low (local eval only) |

**Systemic fix**: `io.LimitReader(resp.Body, 64*1024)` for the HTTP case. For eval logs, stream-parse JSONL instead of loading the full file.

---

## 6. DOCUMENTATION LIES

**CLAUDE.md line 43** describes `internal/knowledge` as "BM25 search". Verified in `engine.go:108-168`: the actual algorithm is **simple substring matching with static field boosts** (title 2.0x, keywords 1.5x, content 1.0x). No IDF, no term frequency normalization, no document length normalization — none of the defining characteristics of BM25.

**Impact**: Anyone reading CLAUDE.md (including future agents) will assume search quality characteristics that don't exist. BM25 implies rare terms score higher than common ones; this implementation doesn't.

**Fix**: Change CLAUDE.md to say "weighted substring search" or "field-boosted text search".

---

## 7. THE "dev" HOSTNAME HEURISTIC

**File**: `tools/workflow_checks_generate.go:185`
```go
if strings.Contains(hostname, "dev") && target.Runtime.EffectiveMode() != workflow.PlanModeSimple {
```

This triggers `deployFiles: [.]` validation for ANY hostname containing "dev" — including `devops`, `developer`, `mydevtool`. In ZCP's context, this is particularly bad because LLMs name services — they might pick `development` or `appdev` as perfectly valid hostnames.

**Fix**: Use the mode metadata (`EffectiveMode() == PlanModeDev`) instead of hostname substring matching. The mode is already available via `target.Runtime`.

---

## 8. VALIDATE MUTATES INPUT

**File**: `workflow/validate.go:215,242`
```go
targets[i].Dependencies[j].Resolution = strings.ToUpper(dep.Resolution)
targets[i].Dependencies[j].Mode = strings.ToUpper(dep.Mode)
```

`ValidateBootstrapTargets` normalizes Resolution and Mode to uppercase directly on the input slice. Go slices are reference types — the caller's data is mutated as a side effect of validation. Any code that calls validate and then inspects the original data will see uppercase values it didn't set.

**Fix**: Either document the mutation contract explicitly, or normalize on a copy.

---

## 9. DEPLOY TARGET PAIRING BUG

**File**: `workflow/deploy_guidance.go:178-182`

`writeTargetSummary` pairs EVERY dev service with the FIRST stage service found in the targets list:
```go
for _, s := range state.Targets {
    if s.Role == DeployRoleStage {
        fmt.Fprintf(sb, " → %s (stage)", s.Hostname)
        break  // always picks the first stage globally
    }
}
```

For multi-runtime deployments (e.g., Go API + Node.js frontend, each with dev/stage pairs), every dev target shows the same stage partner. The LLM receives incorrect pairing guidance.

**Fix**: Match by base hostname (strip dev/stage suffix) or by position in the bootstrap plan targets.

---

## 10. SSH CONFIG PERMISSIONS

**File**: `init/init.go:155`
```go
os.Chmod(tmpName, 0644)
```

SSH config written world-readable. OpenSSH warns about this on some systems, and the config may contain host patterns and connection details.

**Fix**: `0600`.

---

## 11. EVAL CLEANUP BLAST RADIUS

**File**: `eval/cleanup.go:197-224`

`cleanClaudeMemory()` walks `~/.claude/projects/*/memory/` and deletes ALL memory files across ALL Claude Code projects. Called on every eval run (runner.go:113). If someone runs `zcp eval` on a machine where they also use Claude Code for other work, all their project memories are wiped.

**Fix**: Scope to eval project directory only, not all projects.

---

## 12. SELF-UPDATE WITHOUT INTEGRITY CHECK

**File**: `update/apply.go:61-105`

Downloads a binary from GitHub releases and replaces the running `zcp` binary. No checksum, no signature verification. HTTPS provides transport security but not supply-chain protection.

**Context**: This is standard for many Go CLI tools (gh, goreleaser, etc.). The risk is real but industry-standard. Worth noting, not urgent.

---

## 13. TEST INFRASTRUCTURE GAPS

### Permanently skipped realistic test
`integration/bootstrap_realistic_test.go:155` — the full agent flow test is unconditionally skipped. This is the only test that would exercise the checker validation logic through the MCP tool layer. The skip reason: "MCP tool layer tests with checkers require real API or enhanced mock support."

### Bootstrap E2E stops at step 2 of 5
`e2e/bootstrap_workflow_test.go:193` — both fresh and incremental bootstrap E2E tests stop after the provision step. Generate, deploy, and close steps have zero E2E coverage through the bootstrap workflow.

### .zcp state directories in source tree
`.zcp/state/` directories exist under `internal/tools/`, `internal/server/`, `integration/`, and `e2e/`. All are properly .gitignored (`.gitignore:24` has `**/.zcp/`), so this is a local artifact, NOT committed to git. **Not a real issue** — the original audit over-weighted this.

---

## 14. KNOWLEDGE COMPLETENESS vs LIVE DOCS

Compared embedded knowledge (29 recipes, 17 runtimes, 19 guides) against live docs at `docs-1df2.prg1.zerops.app/` and `../zerops-docs/`.

**Accuracy**: 8/10 — version numbers match, YAML schemas correct. Minor: PHP build.base inconsistency (runtime file says `php@X` for build, recipes use `php-nginx@X` for both).

**Gaps**:
- No Flask/FastAPI recipe (Python has only Django)
- No Rust/Deno/Gleam framework recipes (runtimes documented but no recipes)
- No WordPress recipe (mentioned in canonical docs)
- React recipe uses nodejs@20 while all others use @22
- ClickHouse, Qdrant, NATS, Kafka — zero recipe coverage

**Quality**: 9/10 — excellent LLM-optimized format, gotchas sections are high-value, stack layer patterns ideal for progressive agent workflows.

---

## What's NOT Wrong (things the raw audit flagged that are actually fine)

1. **Mock race condition (H2)**: Mock setters are always called during test setup, never concurrently with test execution. The theoretical race window between `getError()` and data read is never hit in practice. Not a real issue.

2. **Eval `--dangerously-skip-permissions` (H3)**: This is the entire point of headless eval mode. Claude CLI needs full autonomy to deploy recipes. The flag is only used in `zcp eval`, a developer tool run manually. Not a vulnerability — it's the intended design.

3. **`umount -l` in eval cleanup (H4)**: Only runs inside `zcp eval cleanup`. Protected paths are properly safeguarded. The function targets `/var/www/` subdirectories on Zerops containers. Not a risk for local dev.

4. **Stale `.zcp/` in source tree (H5)**: Already .gitignored. Local working state, not committed. Not a codebase issue.

5. **Log access token unused (M7)**: The Zerops log backend URL itself contains embedded auth (the URL is dynamically generated with auth baked in). The separate `AccessToken` field appears to be for future use. Not a missing auth step.

6. **`waitForDeletingServices` busy-poll (M9)**: 3-second interval with 5-minute timeout = max 100 API calls. This is waiting for service deletion to complete before re-import. Exponential backoff would be over-engineering for a 100-iteration bounded poll.

---

## Priority Matrix

### Do Now (before next production deployment)
| # | Issue | Effort | File:Line |
|---|-------|--------|-----------|
| 1 | Token sanitization in SSH error path | ~20 lines | `ops/deploy_classify.go:44` |
| 2 | SSH config permissions 0644 → 0600 | 1 line | `init/init.go:155` |
| 3 | Fix CLAUDE.md "BM25" claim | 1 line | `CLAUDE.md:43` |

### Do Soon (next development cycle)
| # | Issue | Effort | Where |
|---|-------|--------|-------|
| 4 | Mutex-during-I/O in getClientID | ~15 lines | `platform/zerops.go:48` |
| 5 | "dev" substring → mode metadata check | ~5 lines | `tools/workflow_checks_generate.go:185` |
| 6 | LimitReader on /status response | 1 line | `ops/verify_checks.go:170` |
| 7 | Deploy target pairing (first-stage bug) | ~15 lines | `workflow/deploy_guidance.go:178` |
| 8 | Scope eval cleanup to eval project only | ~10 lines | `eval/cleanup.go:197` |
| 9 | Sort map keys in guidance generation | ~30 lines (6 locations) | See §3 above |

### Do When Touching the Area
| # | Issue | Where |
|---|-------|-------|
| 10 | Add projectId to API search filters | `platform/zerops.go`, `zerops_search.go` |
| 11 | ValidateBootstrapTargets mutation docs | `workflow/validate.go:215` |
| 12 | Strategy partial-write on error | `tools/workflow_strategy.go:42` |
| 13 | Add Flask/FastAPI/WordPress recipes | `knowledge/recipes/` |
| 14 | Un-skip or replace realistic bootstrap test | `integration/bootstrap_realistic_test.go` |
| 15 | Self-update checksum verification | `update/apply.go` |

---

## Codebase Health Summary

**Overall**: Production-grade. Clean architecture, consistent patterns, good test coverage (250+ tests). The layered design (platform → ops → tools) is sound and well-enforced. The knowledge base is high-quality and purpose-built for LLM consumption.

**Strongest areas**: Platform types and error codes, knowledge engine and recipes, workflow state machine, integration test infrastructure.

**Weakest areas**: SSH deploy error handling (credential exposure), non-deterministic guidance generation, bootstrap E2E coverage gap (steps 3-5 untested).

**Lines audited**: 61,320 Go across 160+ files.
**Real findings**: 15 actionable issues (1 security, 3 correctness, 4 robustness, 2 documentation, 5 test gaps).
**False positives filtered**: 6 findings from raw audit downgraded to "not actually wrong."
