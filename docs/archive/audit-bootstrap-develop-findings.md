# ZCP Bootstrap + Develop Audit — Twice-Verified Findings

**Date**: 2026-04-18
**Scope**: bootstrap (5 steps) and develop workflows across the full 2×4×3 matrix — 2 envs (container/local) × 4 modes (standard/dev/simple/managed-only) × 3 strategies (push-dev/push-git/manual). Includes guidance assembly, work-session lifecycle, adoption paths, and spec-code drift.

**Methodology — three passes:**
1. Seven specialist agents (A–G) produced 75+ raw observations.
2. Round 1 — 31 independent subagents re-verified each consolidated finding; 14 were refuted outright, several reclassified.
3. Round 2 — 16 independent subagents re-verified the survivors against current code; 3 more refuted.

**Final tally:** 13 twice-verified findings (3 CRITICAL · 3 HIGH · 4 MEDIUM · 3 LOW). All code references are `file:line` against HEAD at audit time.

---

## CRITICAL — 3

### F1. `RoleFromMode` missing `env` parameter; local+standard mis-resolves to `simple`

- **Where**: `internal/workflow/briefing.go:67-78` (function); callers at `internal/workflow/briefing.go:49` (`BuildBriefingTargets`) and `internal/workflow/deploy_preflight.go:109` (`preflightRole`).
- **Problem**: Default switch branch returns `DeployRoleSimple` when `stageHostname == ""`. Local+standard's spec invariant is `Hostname=appstage, StageHostname=""` (spec-local-dev.md §6), so the default path fires. Agents see role `simple` instead of `dev` and emit wrong deploy-workflow commands.
- **Why it matters**: Silent correctness bug on every local+standard user. No caller compensates — `engine.Environment()` is threaded into `BuildDevelopBriefing` only *after* `targets[0].Role` is already wrong.
- **Fix sketch**: Extend signature to `RoleFromMode(mode, stageHostname string, env Environment) string`; add an explicit `case PlanModeStandard`; plumb `m.Environment` (already persisted on ServiceMeta) from every caller.

### F2. `Lifecycle Status` block is static for server lifetime (spec claims per-response)

- **Where**: `internal/server/instructions.go` (`BuildInstructions` → `buildWorkflowHint` → `BuildWorkSessionBlock`); registered once at `internal/server/server.go:60` via `mcp.ServerOptions{Instructions: ...}`.
- **Problem**: `spec-work-session.md` §1, §5 and §10.1 state the Lifecycle Status block is "re-generated on every MCP response" so agents can re-orient after context compaction. MCP protocol's `Instructions` field is delivered once at client init and cannot be dynamically re-emitted per tool response. No tool handler injects the block into individual responses either.
- **Why it matters**: The core compaction-survival promise of the work-session design is unfulfillable with the current transport. Agents whose context is trimmed lose lifecycle visibility.
- **Fix direction**: Either (a) rewrite spec to acknowledge the init-only constraint and move per-call state into tool responses, or (b) prepend the block inside every tool handler's text result (~10 lines per handler, makes the spec truthful).

### F3. `handleStrategy` creates orphan incomplete `ServiceMeta` for unknown hostnames

- **Where**: `internal/tools/workflow_strategy.go:50-53`.
- **Problem**: When `ReadServiceMeta` returns `(nil, nil)` for a hostname that was never bootstrapped, the handler builds `&ServiceMeta{Hostname: hostname}` (all other fields zero), sets strategy, and persists. Result: a file with `Mode=""`, `BootstrappedAt=""`, `BootstrapSession=""`. `IsComplete()` returns false; the meta is indistinguishable from an adopted meta. `TestHandleStrategy_NoExistingMeta` encodes this as intended.
- **Why it matters**: Poisons hostname-lock registry; breaks adoption invariants; documented as a known bug in spec-bootstrap-flow-current.md §11.2. Every reader (`briefing.go:50`, `workflow_cicd_generate.go:74`, `instructions_orientation.go:189`, `work_session_hint.go:49`, `router.go:137`) inherits the corruption.
- **Fix sketch**: Reject `action="strategy"` when the hostname is not in `plan.Targets` (or has no complete meta). Do not auto-materialize meta for a hostname ZCP never provisioned.

---

## HIGH — 3

### F4. `InferServicePairing` classifies managed services with a static prefix list

- **Where**: `internal/workflow/adopt.go:30-35` calls `IsManagedService(c.Type)`; definition at `internal/workflow/managed_types.go:34-43` (hardcoded 27-item prefix list).
- **Problem**: Manual adoption (`ValidateBootstrapTargets`) derives managed-ness from `knowledge.ManagedBaseNames(liveTypes)` — the live API catalog. Auto-adoption uses the static list. In `internal/tools/workflow_develop.go`, `liveTypes` is fetched at line 190 *after* `InferServicePairing` has already run on line 182. Any new Zerops managed category ships silent misclassification in auto-adopt until the static list is bumped.
- **Fix sketch**: Reorder `handleDevelopBriefing` to fetch `liveTypes` before `InferServicePairing`, thread it through, and classify via `knowledge.ManagedBaseNames`. Static list becomes pure fallback or is deleted.

### F5. `RecordDeployAttempt` / `RecordVerifyAttempt` accept any hostname without scope check

- **Where**: `internal/workflow/work_session.go:150-202`.
- **Problem**: Neither recorder validates `hostname ∈ ws.Services` before appending to `ws.Deploys[hostname]` / `ws.Verifies[hostname]`. `zerops_verify` (`internal/tools/verify.go:38-52`) also lacks a work-session-scope gate and feeds the hostname straight from the API response into `recordVerifyToWorkSession`.
- **Why it matters**: Work session silently accumulates history for out-of-scope services. `EvaluateAutoClose` only iterates `ws.Services`, so auto-close still works, but when a later session declares overlap with the polluted hostname, stale entries bleed in and skew orientation guidance. Violates single-source-of-truth invariant in spec-work-session.md §7.5.
- **Fix sketch**: Add `inScope(ws, hostname)` precondition to both recorders; return an error if the hostname is outside declared scope. Optional: gate `zerops_verify` with `requireWorkflowContext`.

### F6. Auto-adopt calls `BootstrapCompletePlan` with `liveServices=nil`

- **Where**: `internal/tools/workflow_develop.go:198`.
- **Problem**: `internal/workflow/validate.go:250-253` guards EXISTS resolution behind `if liveServices != nil`, so the check is skipped for auto-adoption. Runtime targets with `IsExisting=true` are never validated against the live catalog; a service deleted between `discover` and `develop` produces a plan referencing a non-existent hostname. The next line `BootstrapComplete("provision", nil, ...)` also passes a nil checker, so there's no compensating control.
- **Why it matters**: Happy path works because `liveServices` is already fetched on line 159 of the same function — the fix is a trivial pass-through. Under the race, plan passes validation and fails cryptically during provisioning.
- **Fix sketch**: `engine.BootstrapCompletePlan(targets, liveTypes, services)` instead of `nil`. Consider adding runtime-target IsExisting validation alongside dependency EXISTS in `validate.go`.

---

## MEDIUM — 4

### F7. Local+standard hostname inversion duplicated across two write paths

- **Where**: `internal/workflow/bootstrap_outputs.go:27-31` (`writeBootstrapOutputs` in-progress writes) and `:75-79` (`writeProvisionMetas` final writes).
- **Problem**: Both blocks perform the same `if e.environment == EnvLocal && stageHostname != ""` swap. No helper. If one is patched and the other isn't, crash recovery mid-bootstrap produces mismatched metas between in-progress and final convention.
- **Fix sketch**: Extract to `invertLocalHostname(env, dev, stage string) (metaHost, stageHost string)` and call from both sites.

### F8. `handleDevelopBriefing` creates WorkSession before the strategy gate

- **Where**: `internal/tools/workflow_develop.go:119-143`.
- **Problem**: Session creation and `RegisterSession` execute unconditionally. Strategy is computed on lines 103-110 and may be empty; no branch short-circuits when strategy is unset. Spec-work-session.md §6.1 mandates "Work session is NOT created yet" when any service has unconfirmed strategy.
- **Why it matters**: Session file leaks at `.zcp/state/work/{pid}.json` until process death or explicit close. Subsequent `action="status"` surfaces a session for a service that hasn't been authorized to deploy.
- **Fix sketch**: Short-circuit and return the strategy-selection briefing before the `NewWorkSession` branch.

### F9. `WriteLocalConfig` is never called; spec claims it's written at generate

- **Where**: `internal/workflow/local_config.go:19-29` (definition); zero production callers (only `local_config_test.go`).
- **Problem**: `spec-local-dev.md §10` states the file is "Created during bootstrap generate step" and guidance uses `.Port` for localhost health-check hints. In practice guidance templates use `{port}` placeholder strings that the agent substitutes — the LocalConfig file never materializes.
- **Fix sketch**: Either implement the write at the end of generate (extract `port` from zerops.yaml `run.ports`), or delete `LocalConfig` and update the spec. Dead code otherwise.

### F10. Empty `BootstrapSession` as "adopted" marker is undocumented and overloaded

- **Where**: `internal/workflow/bootstrap_outputs.go:34-39` and `:82-86` (write sites); consumers at `internal/workflow/service_meta.go:188` (`cleanIncompleteMetasForSession`) and `internal/workflow/engine.go:464` (`checkHostnameLocks`).
- **Problem**: Code convention treats `BootstrapSession == ""` as "service was adopted, not freshly bootstrapped". `spec-workflows.md` describes it as "null for adoption" but code writes empty string. Orphan metas from F3 also produce empty `BootstrapSession`, so the discriminator is ambiguous.
- **Fix sketch**: Document the convention in spec-workflows.md §1.1 ServiceMeta invariants. Add `IsAdoptedService()`: `m.BootstrapSession == "" && m.IsComplete()` — disambiguates adopted metas from F3 orphans.

---

## LOW — 3

### F11. `injectStacks` silently returns content unchanged when markers and anchor both absent

- **Where**: `internal/tools/workflow_bootstrap.go:134-154`.
- **Problem**: Fallback chain is `STACKS:BEGIN/END` markers → `## Part 1` anchor → return unchanged. bootstrap.md has the markers (lines 11-12) but no anchor; current behaviour is correct. Zero unit tests cover any branch. Accidental marker removal (merge, manual edit) would silently ship a stack-less bootstrap response.
- **Fix sketch**: Return error or log warning on the third path; add a table-driven test covering all three branches.

### F12. `GetRecipe` returns fuzzy-match disambiguation as content, not error

- **Where**: `internal/knowledge/briefing.go:105-139`; caller at `internal/tools/knowledge.go:159-166`.
- **Problem**: Multiple fuzzy matches produce `formatDisambiguation(...)` markdown, returned with `err == nil`. Tool handler can't distinguish success from ambiguity and renders the disambiguation as normal content.
- **Why it matters**: The markdown is agent-friendly ("Multiple recipes match …. Use: `zerops_knowledge recipe=…`"), so practical UX is acceptable. Concern is consistency of error handling across the tool layer.
- **Fix sketch (optional)**: Introduce `ErrAmbiguousRecipe` and return via `convertError`. Defer unless tool-layer error handling is unified.

### F13. Skip rules allow pure-adoption plans but spec documents only managed-only

- **Where**: Code at `internal/workflow/bootstrap.go:322-334` (`validateSkip`); spec at `spec-workflows.md §2.8` and invariant B3.
- **Problem**: Code allows skip when `plan==nil` OR `len(Targets)==0` OR `plan.IsAllExisting()`. Spec mentions only managed-only; pure-adoption skip is undocumented. Validation is server-side and correct.
- **Fix sketch**: Doc-only. Update spec-workflows.md §2.8 and invariant B3 to list both conditions; optionally mention pure-adoption in §3.3.

---

## Refuted in Round 2 (dropped from the final list)

- **PlanMode "mixed"**: `resolveStaticGuidance` → `distinctModes(plan)` iterates per-target and extracts each mode's section. `PlanMode()` "mixed" return is only used for cosmetic BootstrapStepInfo — guidance assembly never sees it.
- **SuggestNext delete-ghost after auto-close**: auto-close sets `ws.ClosedAt` and persists; it does *not* delete the file. Next `zerops_develop` hits `workflow_develop.go:121` (`existing.ClosedAt == ""` check), takes the else branch, creates a fresh session cleanly.
- **nil `Checker` panic risk**: `engine.go:250` and `engine_recipe.go:67` both guard with `if checker != nil` before invoking. `buildStepChecker` does return nil for the close step (`workflow_checks.go:51`) — the guards handle it correctly.

## Refuted in Round 1 (not re-verified in Round 2)

ServiceMeta single-row storage (stores both dev+stage fields in one file, not two); `engine.Reset()` correctly preserves sessionID before clearing; `checkHostnameLocks` already has the `isProcessAlive` guard; seven additional MEDIUM/LOW items from agents' raw drafts that didn't survive the first code review.

---

## Remediation order (by blast radius)

1. **F1** — wrong role breaks every local+standard deploy briefing; trivial signature change.
2. **F3** — orphan metas corrupt downstream lock/adoption logic; single-point rejection.
3. **F2** — decide the spec's truth before more agent code is written against the (unfulfillable) per-response promise.
4. **F6** — trivial plumb-through eliminates a real discover→develop race.
5. **F5** — scope-check the two recorders and gate `zerops_verify`.
6. **F4** — reorder fetch + swap to live-types classifier.
7. **F8** — gate session creation on strategy.
8. **F9** — implement or delete `LocalConfig`.
9. **F7, F10** — DRY and docs.
10. **F11, F12, F13** — cosmetic, defer.

---

## Appendix: audit artefacts

- `docs/audit-bootstrap-develop-team-plan.md` — original seven-agent plan.
- `/tmp/zcp_audit/A..G_*.md` — raw specialist agent reports.
- `/tmp/zcp_audit/ROUND2_input.md` — deduped list fed into round 2.

Round-1 verdicts were fed as context into round 2, but round-2 agents re-read current code rather than trusting the round-1 report. Three findings that survived round 1 were refuted by round 2 and dropped.
