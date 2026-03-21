# Bootstrap Flow Redesign — Complete Specification

**Date**: 2026-03-21
**Status**: Design complete, ready for implementation
**Branch**: v2
**Predecessor**: `plans/analysis-bootstrap-flow-gates.review-1.md` (team review), `plans/analysis-bootstrap-flow-gates.v2.md` (initial design)

---

## 1. Motivation

The current 6-step bootstrap flow has three design issues:

1. **Strategy doesn't belong in bootstrap.** Strategy (push-dev / ci-cd / manual) is a deployment maintenance decision, not an infrastructure setup concern. It's ZCP-only metadata — never sent to the Zerops API (verified: zero "strategy" references in `internal/platform/`). Dev/simple modes auto-assign `push-dev` anyway. Forcing the choice inside bootstrap delays completion without adding value.

2. **Deploy and verify have unclear boundaries.** Deploy's checker (`checkDeploy`) only validates API status (RUNNING + subdomain). Verify's checker (`checkVerify`) does full health validation (HTTP endpoints, error logs, startup detection). The deploy guidance tells the agent to verify per-service, but the deploy checker doesn't enforce this. Verify re-runs the same checks the agent already did during deploy.

3. **No post-bootstrap gate for strategy.** Deploy and CI/CD workflows currently have NO strategy check — `handleDeployStart()` never reads `DeployStrategy` from ServiceMeta. There's no enforcement that strategy is set before deploying.

---

## 2. Current State (6 Steps)

```
discover → provision → generate → deploy → verify → strategy
```

### Step checkers — what each validates today

| Step | Checker | What it checks | Enforcement level |
|------|---------|---------------|-------------------|
| discover | Plan validation | Hostnames, types, modes, resolutions against live API catalog | STRONG |
| provision | checkProvision | Services exist + RUNNING + types match + env vars present | STRONG |
| generate | checkGenerate | zerops.yml exists + setup entries + env var refs match discovered | STRONG |
| deploy | checkDeploy | Services RUNNING + subdomains enabled | WEAK (API status only, no health) |
| verify | checkVerify | `ops.VerifyAll()` — HTTP health, error logs, startup detection | STRONG |
| strategy | checkStrategy | Every runtime target has valid strategy assigned | STRONG |

**The problem**: Deploy checker is weak. It doesn't validate that services are actually healthy — just that they're RUNNING in the API. The real health enforcement is deferred to verify. But the agent already verifies per-service during deploy (guidance tells it to), making verify largely redundant as a separate step.

### Key facts (verified by team review)

- **Strategy is ZCP-only**: Zero references in `internal/platform/`. Never sent to Zerops API. Purely workflow metadata.
- **checkStrategy EXISTS**: At `internal/tools/workflow_checks_strategy.go:11-72`, wired at `workflow_checks.go:34`. It works. (Architect review incorrectly claimed it didn't exist.)
- **Deploy has NO strategy gate**: `handleDeployStart()` checks metas are complete (`BootstrappedAt` set) but never reads `DeployStrategy`.
- **Auto-assignment**: `writeBootstrapOutputs()` auto-assigns `push-dev` for dev/simple modes when strategy is empty.
- **Managed-only gap**: `ValidateBootstrapTargets` requires `len(targets) > 0`, blocking managed-only projects. Real bug.
- **CI/CD on Zerops**: Real feature (webhooks, GitHub Actions), but external to import.yml. Setup happens in GUI/repo, not in bootstrap.

---

## 3. Proposed Design (5 Steps)

```
discover → provision → generate → deploy → close
```

### What changes

| Step | Before | After |
|------|--------|-------|
| deploy | Weak checker (API status only) | Strong checker: `VerifyAll()` (full health) |
| verify | Separate step with `VerifyAll()` | **MERGED into deploy checker** |
| strategy | Step 6 of bootstrap | **REMOVED from bootstrap** — standalone post-bootstrap action |
| close | Did not exist | **NEW step** — writes metas, presents strategy choice as next step |

### Step definitions

```go
var stepDetails = []StepDetail{
    {Name: "discover",  Category: CategoryFixed,     Skippable: false},
    {Name: "provision", Category: CategoryFixed,     Skippable: false},
    {Name: "generate",  Category: CategoryCreative,  Skippable: true},
    {Name: "deploy",    Category: CategoryBranching, Skippable: true},
    {Name: "close",     Category: CategoryFixed,     Skippable: true},
}
```

- **close** is `Skippable: true` because managed-only projects skip it (no runtime services → nothing to close).
- **deploy** checker now runs `VerifyAll()` instead of just API status checks. This means deploy = "deploy AND confirm healthy."
- **close** checker is trivial or nil — its job is administrative (write metas, present next steps), not validation.

---

## 4. Deploy Step Redesign

### Why merge verify into deploy

Three independent analysts reviewed the deploy/verify boundary:

1. **step-purist**: Deploy and verify are semantically distinct (infrastructure vs audit) BUT verify's checker re-checks what deploy should have validated. Recommends keeping separate only if boundaries are explicitly documented.

2. **flow-architect**: Deploy guidance already tells agent to verify per-service. Verify step is redundant re-check. Recommends strengthening deploy checker and keeping verify for "batch observability." BUT acknowledges the boundary is unclear.

3. **pragmatist**: Strongest case for keeping separate — iteration semantics, failure granularity, speed tradeoff. BUT admits the redundancy is "intentional safety net, not new work."

**Decision**: Merge verify into deploy. The "safety net" argument doesn't justify a separate step when deploy's checker can do the same validation. The iteration concern is addressed below.

### Deploy checker (strengthened)

Replace current `checkDeploy` (API status only) with:

```go
func checkDeploy(client, fetcher, projectID, httpClient) StepChecker {
    return func(ctx, plan, state) (*StepCheckResult, error) {
        // 1. Run full VerifyAll — HTTP health, error logs, startup detection
        result, err := ops.VerifyAll(ctx, client, fetcher, httpClient, projectID)

        // 2. Filter to plan targets only (ignore pre-existing services)
        planHostnames := buildPlanHostnameSet(plan)

        // 3. Check each plan target is healthy
        // 4. Check subdomains enabled for services with ports
        // 5. Return per-service breakdown in StepCheckResult.Checks
    }
}
```

The checker now validates what the deploy step guidance promises: "deploy code, start servers, enable subdomains, AND confirm everything is healthy."

### Deploy guidance — mode-aware phases

Deploy guidance is structured as sequential phases. The engine doesn't track phases — they're guidance-level organization that the agent follows. The checker validates the end state.

#### Standard mode

```markdown
## Phase 1: Deploy Dev
- zerops_deploy targetService={devHostname}
- Start server via SSH (Bash run_in_background=true)
- zerops_subdomain action="enable" serviceHostname={devHostname}
- zerops_verify serviceHostname={devHostname} — confirm health before proceeding

## Phase 2: Deploy Stage
- Write stage entry in zerops.yml (real start command, healthCheck required)
- zerops_deploy sourceService={devHostname} targetService={stageHostname}
- zerops_manage action="connect-storage" (if shared-storage)
- zerops_subdomain action="enable" serviceHostname={stageHostname}
- zerops_verify serviceHostname={stageHostname}

## Phase 3: Cross-Verify
- zerops_verify (batch, all services) — final end-to-end health check
- Confirm /status endpoints report all dependency connections OK

## Complete
- action="complete" step="deploy" attestation="All services deployed and verified healthy"
```

#### Dev mode

```markdown
## Phase 1: Deploy Dev
- zerops_deploy targetService={devHostname}
- Start server via SSH
- zerops_subdomain action="enable"
- zerops_verify serviceHostname={devHostname}

## Phase 2: Cross-Verify
- zerops_verify (batch)

## Complete
- action="complete" step="deploy"
```

#### Simple mode

```markdown
## Phase 1: Deploy
- zerops_deploy targetService={hostname} (server auto-starts via healthCheck)
- zerops_subdomain action="enable"
- zerops_verify serviceHostname={hostname}

## Phase 2: Cross-Verify
- zerops_verify (batch)

## Complete
- action="complete" step="deploy"
```

### Iteration with merged deploy

**Current**: `ResetForIteration()` resets steps 2-4 (generate, deploy, verify).
**New**: `ResetForIteration()` resets steps 2-3 (generate, deploy). Same effect — agent retries code generation and deployment, including the now-built-in health verification.

If an agent fails deploy's checker (health check fails), it can iterate — which resets generate+deploy. If the issue is just a health flake, the agent doesn't need to regenerate code — it can redeploy and the checker re-runs VerifyAll.

### Deploy checker output — per-service clarity

Even without sub-steps, the checker provides per-service breakdown:

```json
{
  "passed": false,
  "checks": [
    {"name": "appdev_health", "status": "pass", "detail": "healthy"},
    {"name": "appstage_health", "status": "fail", "detail": "HTTP /status returned 503"},
    {"name": "appdev_subdomain", "status": "pass"},
    {"name": "appstage_subdomain", "status": "pass"},
    {"name": "db_health", "status": "pass", "detail": "healthy"}
  ],
  "summary": "appstage unhealthy"
}
```

Agent sees exactly what failed. No ambiguity despite flat step structure.

---

## 5. Close Step

### Purpose

Administrative closure of bootstrap. No business logic — just:
1. Write final ServiceMeta files (with `BootstrappedAt` timestamp)
2. Auto-assign `push-dev` strategy for dev/simple modes
3. Append reflog entry to CLAUDE.md
4. Present strategy-aware transition message

### Checker

Trivial or nil. Close doesn't validate infrastructure — it records outcomes.

### Transition message (strategy-aware)

The close step's response includes a transition message that varies by mode:

**Dev/simple modes** (strategy auto-assigned to `push-dev`):
```
Bootstrap complete.

## Services
- **appdev** (nodejs@22, dev mode) — push-dev strategy auto-assigned

## Next Steps
Infrastructure is ready. Start deploying:
→ zerops_workflow action="start" workflow="deploy"

Other operations: scale, debug, configure
```

**Standard mode** (strategy NOT auto-assigned):
```
Bootstrap complete.

## Services
- **appdev** (nodejs@22, standard mode)
  Stage: **appstage**

## Choose Deployment Strategy
Before deploying, choose how each service will be maintained:
  - push-dev: SSH push to dev container (prototyping, iteration)
  - ci-cd: Git pipeline trigger (teams, production)
  - manual: No automation (existing pipelines)

→ zerops_workflow action="strategy" strategies={"appdev":"ci-cd"}

After choosing, start deploy or CI/CD workflow.
```

**Mixed modes** (some auto-assigned, some need choice):
```
Bootstrap complete.

## Services
- **apidev** (go@1, dev mode) — push-dev strategy auto-assigned ✓
- **webdev** (nodejs@22, standard mode) — strategy required

## Choose Deployment Strategy for Standard-Mode Services
→ zerops_workflow action="strategy" strategies={"webdev":"ci-cd"}

Dev-mode services are ready for deploy immediately.
```

**Managed-only** (no runtime services):
```
Bootstrap complete.

## Services
- **db** (postgresql@16) — running
- **cache** (valkey@7.2) — running

All managed services are operational. No deployment strategy needed.
Other operations: scale, debug, configure
```

---

## 6. Post-Bootstrap Strategy Gate

### Current state

Deploy and CI/CD workflows have NO strategy check. `handleDeployStart()` reads ServiceMeta for hostnames and modes but never checks `DeployStrategy`.

### Proposed: Deploy workflow gate

In `handleDeployStart()` (`internal/tools/workflow.go`), after filtering runtime metas:

```go
for _, m := range runtimeMetas {
    if m.DeployStrategy == "" {
        return error: platform.NewPlatformError(
            platform.ErrInvalidParameter,
            fmt.Sprintf("Strategy not set for %q", m.Hostname),
            "Set strategy first: zerops_workflow action=strategy strategies={\""+m.Hostname+"\":\"push-dev|ci-cd|manual\"}")
    }
}
```

### Proposed: CI/CD workflow gate

In `handleCICDStart()`, check at least one service has `ci-cd` strategy:

```go
hasCICD := false
for _, m := range metas {
    if m.DeployStrategy == workflow.StrategyCICD {
        hasCICD = true
    }
}
if !hasCICD {
    return error: "No services have ci-cd strategy. Set strategy first."
}
```

### Proposed: Router enhancement

In `Route()` (`internal/workflow/router.go`), when any runtime meta has empty `DeployStrategy`:

```go
if needsStrategy {
    offerings = prepend(offerings, FlowOffering{
        Workflow: "strategy",
        Priority: 0,
        Reason:   "Deployment strategy not set — required before deploy or CI/CD",
        Hint:     `zerops_workflow action="strategy" strategies={"hostname":"push-dev|ci-cd|manual"}`,
    })
}
```

This ensures the router promotes strategy selection before deploy/cicd.

---

## 7. Mode Gate (Defense-in-Depth)

Add explicit `Plan != nil` check in `BootstrapComplete()` for non-discover steps:

```go
if stepName != StepDiscover && state.Bootstrap.Plan == nil {
    return nil, fmt.Errorf("bootstrap complete: step %q requires plan from discover step", stepName)
}
```

Step ordering already prevents this (discover is non-skippable, step 0). This is defense-in-depth — makes the invariant explicit.

---

## 8. Managed-Only Bootstrap Fix

`ValidateBootstrapTargets` requires `len(targets) > 0`, blocking managed-only projects (zero runtime services, only databases/caches).

**Fix**: Allow empty targets array. Managed-only projects:
- Submit plan with empty `targets` and dependencies at top level (or via a separate mechanism)
- Skip generate + deploy + close (no runtime services)
- Complete: discover → provision → [DONE]

**Minimal change**: Remove `len(targets) == 0` check in `validate.go:128`. The managed-only fast path (`validateConditionalSkip`) already correctly skips generate/deploy.

---

## 9. Complete Bootstrap Flow by Mode

### Standard mode

```
discover    → identify services, choose standard mode, submit plan
provision   → import services (dev+stage+managed), mount dev, discover env vars
generate    → write zerops.yml (dev entry) + app code on SSHFS mount
deploy      → Phase 1: deploy dev → SSH start → subdomain → verify
              Phase 2: write stage entry → deploy stage → subdomain → verify
              Phase 3: cross-verify all services
              Checker: VerifyAll (all healthy + subdomains enabled)
close       → write metas (strategy=""), present "choose strategy for standard services"
              → user calls action="strategy" → then starts deploy/cicd workflow
```

### Dev mode

```
discover    → identify services, choose dev mode, submit plan
provision   → import services (dev+managed), mount dev, discover env vars
generate    → write zerops.yml (dev entry) + app code
deploy      → Phase 1: deploy dev → SSH start → subdomain → verify
              Phase 2: cross-verify
              Checker: VerifyAll
close       → write metas (strategy="push-dev" auto-assigned), present "done → deploy"
```

### Simple mode

```
discover    → identify services, choose simple mode, submit plan
provision   → import service + managed, mount service, discover env vars
generate    → write zerops.yml (real start, healthCheck) + app code
deploy      → Phase 1: deploy (auto-starts) → subdomain → verify
              Phase 2: cross-verify
              Checker: VerifyAll
close       → write metas (strategy="push-dev" auto-assigned), present "done → deploy"
```

### Managed-only

```
discover    → identify managed services, submit plan (empty targets)
provision   → import managed services, discover env vars
generate    → SKIP (no runtime services)
deploy      → SKIP (no runtime services)
close       → SKIP (or: write metas for managed services, present "done")
```

---

## 10. Implementation Plan

### Phase 1: Structural changes (TDD)

| # | Task | Files | Tests |
|---|------|-------|-------|
| 1 | Fix managed-only validation | `validate.go` | Add managed-only test case to `validate_test.go` |
| 2 | Replace strategy with close in step list | `bootstrap_steps.go` | Update `bootstrap_test.go` step count assertions |
| 3 | Update iteration reset | `bootstrap.go` (ResetForIteration) | Update iteration tests |
| 4 | Add mode gate | `engine.go` | Add Plan-nil test to `engine_test.go` |

### Phase 2: Deploy checker strengthening (TDD)

| # | Task | Files | Tests |
|---|------|-------|-------|
| 5 | Merge VerifyAll into checkDeploy | `workflow_checks.go` | Update `workflow_checks_deploy_test.go` |
| 6 | Remove separate checkVerify | `workflow_checks.go` | Remove verify checker tests |
| 7 | Add close checker (trivial/nil) | `workflow_checks.go` | Test close step completion |

### Phase 3: Transition and gates (TDD)

| # | Task | Files | Tests |
|---|------|-------|-------|
| 8 | Redesign BuildTransitionMessage | `bootstrap_guide_assembly.go` | Test all 4 mode variants |
| 9 | Add deploy strategy gate | `workflow.go` (handleDeployStart) | Test deploy blocked without strategy |
| 10 | Add CI/CD strategy gate | `workflow.go` (handleCICDStart) | Test cicd blocked without ci-cd strategy |
| 11 | Add router strategy offering | `router.go` | Test p0 offering when strategy empty |

### Phase 4: Cleanup

| # | Task | Files |
|---|------|-------|
| 12 | Delete strategy checker | `workflow_checks_strategy.go` (DELETE), `workflow_checks.go` (remove case) |
| 13 | Delete strategy checker tests | `workflow_checks_strategy_test.go` (DELETE) |
| 14 | Update bootstrap.md content | `internal/content/workflows/bootstrap.md` (remove strategy section, update deploy guidance with phases) |
| 15 | Update spec | `docs/spec-bootstrap-deploy.md` |
| 16 | Update close step skip guards | `bootstrap.go` (validateConditionalSkip — add close skip logic for managed-only) |

### Files summary

| File | Action |
|------|--------|
| `internal/workflow/bootstrap_steps.go` | Replace StepStrategy with StepClose |
| `internal/workflow/bootstrap.go` | Update step constants, skip guards, iteration reset range |
| `internal/workflow/bootstrap_outputs.go` | Move to close step trigger (was verify completion) |
| `internal/workflow/bootstrap_guide_assembly.go` | Redesign BuildTransitionMessage (strategy-aware, 4 mode variants) |
| `internal/workflow/engine.go` | Add Plan!=nil check, update close step handling |
| `internal/workflow/router.go` | Add p0 "set strategy" offering when DeployStrategy empty |
| `internal/workflow/validate.go` | Allow len(targets)==0 for managed-only |
| `internal/tools/workflow.go` | Add strategy gates in handleDeployStart + handleCICDStart |
| `internal/tools/workflow_checks.go` | Merge VerifyAll into checkDeploy, add checkClose (trivial), remove checkVerify + checkStrategy cases |
| `internal/tools/workflow_checks_strategy.go` | DELETE |
| `internal/tools/workflow_checks_strategy_test.go` | DELETE |
| `internal/content/workflows/bootstrap.md` | Remove strategy section, restructure deploy guidance with Phase markers |
| `docs/spec-bootstrap-deploy.md` | Update to 5-step (discover, provision, generate, deploy, close) |

### What stays unchanged

- `action="strategy"` tool handler — standalone, no session needed
- `handleStrategy()` in `workflow_strategy.go` — reads/writes ServiceMeta directly
- Strategy validation (`validStrategies` map) — still validates push-dev/ci-cd/manual
- `StrategyToSection` map — still drives deploy guidance extraction
- CI/CD workflow — separate 3-step workflow, unchanged
- ServiceMeta struct — `DeployStrategy` field stays
- Deploy workflow — unchanged (reads metas, builds targets)
- `ops.VerifyAll()` — unchanged (called from deploy checker now instead of verify checker)

---

## 11. Design Decisions Log

| # | Decision | Why | Alternative considered | Why rejected |
|---|----------|-----|----------------------|--------------|
| 1 | Remove strategy from bootstrap | ZCP-only metadata, not infrastructure. Auto-assign handles dev/simple. | Keep strategy as step 6 (DX/Product recommended) | User explicitly wants separation. DX coherence concern addressed by close step's transition message. |
| 2 | Merge verify into deploy | Deploy checker should validate deploy's output. Verify re-checks the same things. | Keep verify separate (pragmatist recommended for iteration/granularity) | Iteration works fine with merged steps. Per-service checker breakdown provides granularity. |
| 3 | Flat steps, not sub-steps | Engine stays simple. Sub-steps require new architecture (nested state, sub-step API). | Engine-level sub-steps for deploy (mode-aware) | Guidance-level phases achieve same clarity without architecture change. Checker per-service breakdown provides visibility. |
| 4 | Close as explicit step | Gives agent a clear "bootstrap done" signal. Separates infrastructure work from administrative closure. | Auto-close at deploy completion | Explicit step allows managed-only skip, clean transition message, strategy prompting. |
| 5 | Deploy strategy gate (NEW) | Deploy should know HOW to deploy. Strategy-first, then deploy. | No gate (current behavior) | Without gate, user deploys without choosing strategy, then has no CI/CD path. |
| 6 | Managed-only fix | Managed-only projects are valid Zerops use case. `len(targets)>0` blocks them. | Require dummy runtime target | Hack, not a fix. |

---

## 12. Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Existing tests break (step count change) | HIGH | LOW | TDD: write failing tests first, then fix |
| Deploy checker slower (VerifyAll takes 5-15s/service) | MEDIUM | LOW | Already happens at verify step today. Just moved earlier. |
| Agent confused by missing verify step | LOW | LOW | Guidance phases make deploy's scope clear |
| Strategy gate blocks existing deploy workflows | LOW | MEDIUM | Only blocks when DeployStrategy="" (standard mode without explicit choice). Dev/simple auto-assigned. |
| Iteration reset range change (2-4 → 2-3) | LOW | LOW | Same effect: agent retries generate+deploy. Close is administrative, not retried. |

---

## 13. Spec-vs-Implementation Discrepancies Found During Review

| # | Spec claim | Implementation | Status |
|---|-----------|----------------|--------|
| 1 | Strategy is step 6 of 6 | checkStrategy exists and enforces | MATCH (being changed) |
| 2 | Managed-only: "code gap" | `validate.go:128` blocks empty targets | CONFIRMED GAP — fix in Phase 1 |
| 3 | Mode chosen in discover | Plan submitted via BootstrapCompletePlan | MATCH |
| 4 | Strategy auto-assigned dev/simple | `bootstrap_outputs.go:27-29` | MATCH |
| 5 | Deploy checker validates health | checkDeploy only checks API status | MISMATCH — being fixed |
| 6 | Verify step is independent audit | checkVerify calls VerifyAll | MATCH (being merged) |
