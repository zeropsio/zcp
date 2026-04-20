# implementation-notes.md — zcprecipator2 implementation phase running notes

Running commit-by-commit notes kept inline during cleanroom execution per
[`06-migration/rollout-sequence.md`](06-migration/rollout-sequence.md). Each
commit appends a section; nothing is retroactively rewritten.

---

## C-0 — Baseline: operational substrate untouched verification

**Status**: green

**Regression floor**: `go test ./... -count=1 -short` passes across 19
packages (verified before any edit landed).

### Substrate-invariant coverage audit

The C-0 plan named 8 test files to add. Audit against existing coverage
shows substrate invariants are already comprehensively pinned:

| Plan-named invariant | Plan-named file | Current pin |
|---|---|---|
| SSH boundary — no `cd /var/www/{host} && <exec>` bash emission; bash_guard rejects [v17/v8.80] | `internal/platform/ssh_test.go` | [`internal/tools/bash_guard_test.go`](../../internal/tools/bash_guard_test.go) — 168 lines exercising 11 executable tokens + ssh-wrap peel + edge cases |
| `SUBAGENT_MISUSE` on sub-agent calling `zerops_workflow action=start` [v8.90] | `internal/tools/workflow_subagent_misuse_test.go` | [`internal/tools/workflow_start_test.go`](../../internal/tools/workflow_start_test.go) — 245 lines + [`internal/workflow/recipe_close_ordering_test.go`](../../internal/workflow/recipe_close_ordering_test.go) + [`internal/workflow/recipe_tool_use_policy_test.go`](../../internal/workflow/recipe_tool_use_policy_test.go) |
| Single container-side `git config + git init` call shape [v8.93.1] | `internal/workflow/git_config_mount_test.go` | [`internal/init/init_container_test.go`](../../internal/init/init_container_test.go) — `TestContainerSteps_GitConfig` locks the exact 3-command shape; idempotence + outside-container skip also covered |
| `FactRecord.Scope` enum values + filter behavior [v8.96 Theme B] | `internal/ops/facts_log_test.go` | **GAP FILLED in this commit** — added `TestFactsLog_AllScopesAccepted` + `TestFactsLog_RejectsUnknownScope` + `TestFactsLog_ScopeRoundTrip` to [`internal/ops/facts_log_test.go`](../../internal/ops/facts_log_test.go) |
| Env-README Go-template byte-for-byte output for 6 canonical env tiers [v8.95 Fix B] | `internal/workflow/recipe_templates_test.go` | [`internal/workflow/recipe_templates_test.go`](../../internal/workflow/recipe_templates_test.go) — 1734 lines exercising every tier and dual-runtime permutation; `recipe_templates_dualruntime_test.go` + `recipe_templates_project_env_test.go` extend |
| Edit tool rejects edits to unread files [v8.97 Fix 3] | `internal/tools/workflow_read_before_edit_test.go` | **N/A** — this is Claude Code's built-in Edit-tool guard, not a zcp MCP surface. No zcp code to pin. Marked out of scope for C-0 (see rollout-sequence C-0 rationale). |
| `ExportRecipe` refuses when close step incomplete [v8.97 Fix 1] | `internal/sync/export_test.go` | [`internal/sync/export_gate_test.go`](../../internal/sync/export_gate_test.go) — 254 lines covering close-incomplete diagnostics + user-gated wording |
| Current pre-rewrite close `NextSteps=[…export/publish]` (pin before C-11 flip) | `internal/tools/workflow_close_next_steps_test.go` | [`internal/workflow/recipe_test.go`](../../internal/workflow/recipe_test.go) — `TestHandleComplete_CloseStepReturnsPostCompletionGuidance` (asserts `len(nextSteps)==2` at v8.103 shape) + `TestHandleComplete_CloseStepPostCompletionBothUserGated` + `TestHandleComplete_CloseStepSummaryHasNoAutomaticClaims`. These tests will deliberately break on C-11; that's by design. |

### Coverage decision

Creating duplicate test files under the plan-named paths would replicate
existing coverage without strengthening the regression floor. The audit
above shows every substrate invariant (except the genuine `FactRecord.Scope`
gap) is already pinned by a comprehensive test file. For C-0, adding the
three Scope tests closes the one real gap; the remaining invariants are
locked by pre-existing tests that any future commit must keep green.

### What landed

- [`internal/ops/facts_log_test.go`](../../internal/ops/facts_log_test.go): +94 LoC
  - `TestFactsLog_AllScopesAccepted` — 4 valid scope values round-trip
  - `TestFactsLog_RejectsUnknownScope` — typo'd scope rejected with enumerated valid values in error message
  - `TestFactsLog_ScopeRoundTrip` — marshal/unmarshal preserves Scope field
- `docs/zcprecipator2/implementation-notes.md` — this file

### Verification

- `go test ./internal/ops/... -count=1 -run 'TestFactsLog_'` — 10 scope-related tests green (3 new + 7 existing)
- `go test ./... -count=1 -short` — full suite green, identical to pre-C-0 baseline (regression floor preserved)

### LoC delta

- Tests: +94 LoC
- Docs: +65 lines

### Breaks-alone consequence

Nothing. C-0 is additive: three new table-driven test functions that lock
an invariant already enforced in production code (`facts_log.go:95`
`knownScopes[rec.Scope]` check). The regression floor is the existing
full test suite, which remains green.

### Ordering deps verified

None — C-0 is the baseline.
