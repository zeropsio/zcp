# Bootstrap Flow Redesign — Complete Specification (v2)

**Date**: 2026-03-21
**Status**: Design reviewed, implementation-ready
**Branch**: v2
**Predecessor**: `plans/bootstrap-flow-redesign.md` (v1), `bootstrap-flow-redesign.review-1.md`

---

## 1. Motivation

The current 6-step bootstrap flow has three design issues:

1. **Strategy doesn't belong in bootstrap.** Strategy (push-dev / ci-cd / manual) is a deployment maintenance decision, not an infrastructure setup concern. It's ZCP-only metadata — never sent to the Zerops API (verified: zero "strategy" references in `internal/platform/`). Forcing the choice inside bootstrap delays completion without adding value. Strategy is always an explicit user decision — no auto-assignment.

2. **Deploy and verify have unclear boundaries.** Deploy's checker (`checkDeploy`) only validates API status (RUNNING + subdomain). Verify's checker (`checkVerify`) does full health validation (HTTP endpoints, error logs, startup detection). The deploy guidance tells the agent to verify per-service, but the deploy checker doesn't enforce this. Verify re-runs the same checks the agent already did during deploy.

3. **No post-bootstrap gate for strategy.** Deploy and CI/CD workflows currently have NO strategy check — `handleDeployStart()` never reads `DeployStrategy` from ServiceMeta.

---

## 2. Proposed Design (5 Steps)

```
discover → provision → generate → deploy → close
```

### Step definitions

```go
const (
    StepDiscover  = "discover"
    StepProvision = "provision"
    StepGenerate  = "generate"
    StepDeploy    = "deploy"
    StepClose     = "close"
)

var stepDetails = []StepDetail{
    {Name: StepDiscover,  Skippable: false, Tools: []string{"zerops_discover", "zerops_knowledge", "zerops_workflow"},
     Verification: "SUCCESS WHEN: plan submitted via zerops_workflow action=complete step=discover with valid targets"},
    {Name: StepProvision, Skippable: false, Tools: []string{"zerops_import", "zerops_process", "zerops_discover", "zerops_mount"},
     Verification: "SUCCESS WHEN: all plan services exist in API with RUNNING status AND types match AND env vars recorded"},
    {Name: StepGenerate,  Skippable: true,  Tools: []string{"zerops_knowledge"},
     Verification: "SUCCESS WHEN: zerops.yml exists with setup entry for each target AND env var refs match discovered"},
    {Name: StepDeploy,    Skippable: true,  Tools: []string{"zerops_deploy", "zerops_discover", "zerops_subdomain", "zerops_logs", "zerops_mount", "zerops_verify", "zerops_manage"},
     Verification: "SUCCESS WHEN: all runtime services deployed, accessible, AND healthy (VerifyAll: HTTP, logs, startup, subdomains)"},
    {Name: StepClose,     Skippable: true,  Tools: []string{"zerops_workflow"},
     Verification: "SUCCESS WHEN: bootstrap administratively closed (metas written, transition presented)"},
}
```

### What changed from 6-step

| Change | Before | After |
|--------|--------|-------|
| deploy checker | Weak (API status only) | Strong: VerifyAll (full health) |
| verify step | Separate step with VerifyAll | Merged into deploy checker |
| strategy step | Step 6 of bootstrap | Removed — standalone post-bootstrap |
| close step | Did not exist | New — administrative closure trigger |

---

## 3. Deploy Step Redesign

### Deploy checker (strengthened)

```go
func checkDeploy(client, fetcher, projectID, httpClient) StepChecker {
    return func(ctx, plan, state) (*StepCheckResult, error) {
        if plan == nil {
            return nil, nil
        }
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

Note: `buildStepChecker` already receives `fetcher` and `httpClient` params — just wire them to checkDeploy.

### Deploy guidance — mode-aware phases

Deploy guidance is structured as sequential phases. The engine doesn't track phases — they're guidance-level organization. The checker validates the end state.

#### Standard mode

```markdown
## Phase 1: Deploy Dev
- zerops_deploy targetService={devHostname}
- Start server via SSH (Bash run_in_background=true)
- zerops_subdomain action="enable" serviceHostname={devHostname}
- zerops_verify serviceHostname={devHostname}

## Phase 2: Deploy Stage
- Write stage entry in zerops.yml (real start command, healthCheck required)
- zerops_deploy sourceService={devHostname} targetService={stageHostname}
- zerops_manage action="connect-storage" (if shared-storage)
- zerops_subdomain action="enable" serviceHostname={stageHostname}
- zerops_verify serviceHostname={stageHostname}

## Phase 3: Cross-Verify
- zerops_verify (batch, all services)

## Complete
- action="complete" step="deploy" attestation="All services deployed and verified healthy"
```

#### Dev mode / Simple mode

Single-phase deploy + cross-verify. *(Same as v1)*

### Iteration with merged deploy

```go
// ResetForIteration: explicit range 2-3 (generate + deploy).
// Close(4) is administrative — NOT retried on iteration.
for i := 2; i <= 3 && i < len(b.Steps); i++ {
    b.Steps[i] = BootstrapStep{Name: b.Steps[i].Name, Status: stepPending}
}
b.CurrentStep = 2
```

### Deploy checker output

Per-service breakdown in StepCheckResult.Checks:

```json
{
  "passed": false,
  "checks": [
    {"name": "appdev_health", "status": "pass", "detail": "healthy"},
    {"name": "appstage_health", "status": "fail", "detail": "HTTP /status returned 503"},
    {"name": "appdev_subdomain", "status": "pass"},
    {"name": "appstage_subdomain", "status": "pass"}
  ],
  "summary": "appstage unhealthy"
}
```

---

## 4. Close Step

### Purpose

Administrative closure trigger. Close step completion makes `Active=false`, which triggers the existing `writeBootstrapOutputs` in `engine.go`. No duplication of meta-write logic.

What `writeBootstrapOutputs` does (triggered by `!Active`):
1. Writes final ServiceMeta files (with `BootstrappedAt` timestamp, strategy stays empty)
2. Appends reflog entry to CLAUDE.md

Strategy is NEVER auto-assigned. All modes require explicit `action=strategy` after bootstrap.

### Checker

Nil. Close doesn't validate infrastructure — it's the administrative trigger point.

### Transition message (strategy-aware)

The close step's **response** includes a mode-variant transition message via redesigned `BuildTransitionMessage`:

**All runtime modes** (strategy always required):
```
Bootstrap complete.

## Services
- **appdev** (nodejs@22, dev mode)

## Choose Deployment Strategy
Before deploying, choose strategy for each service:
  - push-dev: SSH push (prototyping, iteration)
  - ci-cd: Git pipeline (teams, production)
  - manual: No automation (existing pipelines)

→ zerops_workflow action="strategy" strategies={"appdev":"push-dev"}

After choosing, start deploy or CI/CD workflow.
```

**Standard mode with stage**:
```
Bootstrap complete.

## Services
- **appdev** (nodejs@22, standard mode)
  Stage: **appstage**

## Choose Deployment Strategy
→ zerops_workflow action="strategy" strategies={"appdev":"ci-cd"}
```

**Managed-only** (close is SKIPPED — this message comes from bootstrap completion):
```
Bootstrap complete.

## Services
- **db** (postgresql@16) — running
- **cache** (valkey@7.2) — running

All managed services operational. No deployment needed.
```

---

## 5. Strategy Selection in Deploy Workflow

### Design principle

Strategy selection is NOT a blocker — it's a **conversational gate**. When a service has no strategy set, the deploy workflow doesn't throw an error. Instead, it presents the choice, helps the user decide, and then continues with deploy.

### Deploy workflow: strategy check + inline selection

In `handleDeployStart()`, after filtering runtime metas, check for missing strategies:

```go
var needStrategy []*workflow.ServiceMeta
for _, m := range runtimeMetas {
    if m.DeployStrategy == "" {
        needStrategy = append(needStrategy, m)
    }
}
if len(needStrategy) > 0 {
    return strategySelectionResponse(needStrategy)
}
// All strategies set — proceed with deploy
```

The `strategySelectionResponse` returns guidance (not an error) that presents all three options equally and explains the differences:

```
## How should this service be deployed?

You haven't chosen a deployment strategy for **appdev** yet. There are three options — each works differently:

### push-dev
You deploy by pushing code from a dev container via SSH. This is what happened during bootstrap.
- **How it works**: You (or the agent) edit code on the dev container, then `zcli push` deploys it. Fast feedback loop.
- **Good for**: Prototyping, experimenting, quick iterations where you're actively developing.
- **Trade-off**: Manual process — you trigger each deploy yourself.

### ci-cd
Deploys happen automatically when you push to a git repository.
- **How it works**: You connect a GitHub/GitLab repo. Every push triggers a build and deploy on Zerops via webhook.
- **Good for**: Team development, production workflows, any project where you want deploys tied to git history.
- **Trade-off**: Requires initial pipeline setup (I can help with that).

### manual
No automated deployment. You manage the deployment process yourself.
- **How it works**: Zerops runs your service, but you handle deploys with your own tools or process.
- **Good for**: Projects with existing CI/CD pipelines, custom deployment workflows, or when Zerops is just the runtime.
- **Trade-off**: ZCP won't manage or guide your deploys.

→ Choose: `zerops_workflow action="strategy" strategies={"appdev":"push-dev|ci-cd|manual"}`

After choosing, re-run: `zerops_workflow action="start" workflow="deploy"`
```

This is not an error — it's the deploy workflow helping the user make an informed decision.

### CI/CD workflow: requires explicit ci-cd strategy

CI/CD is intentionally stricter — it's a conscious infrastructure choice:

```go
var cicdServices []string
for _, m := range metas {
    if m.DeployStrategy == workflow.StrategyCICD {
        cicdServices = append(cicdServices, m.Hostname)
    }
}
if len(cicdServices) == 0 {
    return convertError(platform.NewPlatformError(
        platform.ErrInvalidParameter,
        "No services have ci-cd strategy",
        "Set ci-cd strategy first: zerops_workflow action=strategy strategies={\"hostname\":\"ci-cd\"}"))
}
```

### Deploy workflow respects chosen strategy

Once strategy is set, the deploy workflow uses it to determine HOW to deploy:

| Strategy | Deploy behavior |
|----------|----------------|
| `push-dev` | SSH push to dev container, start server, verify |
| `ci-cd` | Guide user to set up git pipeline (webhook/GitHub Actions) |
| `manual` | Skip automated deploy, just verify current state |

The deploy guidance already has strategy-specific sections in `deploy.md` (`deploy-push-dev`, `deploy-ci-cd`, `deploy-manual`) extracted via `StrategyToSection` map.

### Router enhancement

When any runtime meta has empty `DeployStrategy`, prepend p0 offering suggesting strategy selection.

---

## 6. Mode Gate (Defense-in-Depth)

Add explicit `Plan != nil` check in `BootstrapComplete()` for non-discover steps:

```go
if stepName != StepDiscover && state.Bootstrap.Plan == nil {
    return nil, fmt.Errorf("bootstrap complete: step %q requires plan from discover step", stepName)
}
```

---

## 7. Managed-Only Bootstrap Fix

Remove `len(targets) == 0` check in `validate.go:128`. Managed-only projects:
- Submit plan with empty `targets` (dependencies at top level or separate mechanism)
- Skip generate + deploy + close (no runtime services)
- Complete: discover → provision → DONE

The managed-only fast path (`validateConditionalSkip`) needs a close guard:

```go
case stepGenerate, stepDeploy, "close":
    if hasRuntimeServices {
        return fmt.Errorf("skip step: cannot skip %q — runtime services in plan require it", stepName)
    }
```

---

## 8. Complete Bootstrap Flow by Mode

### Standard mode
```
discover    → identify services, choose standard mode, submit plan
provision   → import services (dev+stage+managed), mount dev, discover env vars
generate    → write zerops.yml (dev entry) + app code on SSHFS mount
deploy      → Phase 1: deploy dev → SSH start → subdomain → verify
              Phase 2: write stage entry → deploy stage → subdomain → verify
              Phase 3: cross-verify all services
              Checker: VerifyAll (all healthy + subdomains enabled)
close       → [trigger] writeBootstrapOutputs (strategy=""), present "choose strategy"
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
close       → [trigger] writeBootstrapOutputs (strategy=""), present "choose strategy"
              → user calls action="strategy" → then starts deploy workflow
```

### Simple mode
```
discover    → identify services, choose simple mode, submit plan
provision   → import service + managed, mount service, discover env vars
generate    → write zerops.yml (real start, healthCheck) + app code
deploy      → Phase 1: deploy (auto-starts) → subdomain → verify
              Phase 2: cross-verify
              Checker: VerifyAll
close       → [trigger] writeBootstrapOutputs (strategy=""), present "choose strategy"
              → user calls action="strategy" → then starts deploy workflow
```

### Managed-only
```
discover    → identify managed services, submit plan (empty targets)
provision   → import managed services, discover env vars
generate    → SKIP (no runtime services)
deploy      → SKIP (no runtime services)
close       → SKIP (no runtime services)
```

---

## 9. Implementation Plan

### Phase 1: Structural changes (TDD)

| # | Task | Files | Tests |
|---|------|-------|-------|
| 1 | Fix managed-only validation (allow empty targets) | `validate.go` | Add managed-only test case to `validate_test.go` |
| 2 | Replace StepStrategy/StepVerify with StepClose in step list | `bootstrap_steps.go` | Update step count + name assertions |
| 3 | Update iteration reset range to 2-3 | `bootstrap.go` (ResetForIteration) | Verify close NOT reset, generate+deploy ARE reset |
| 4 | Add close + generate + deploy skip guard | `bootstrap.go` (validateConditionalSkip) | Test managed-only skips all three |
| 5 | Add mode gate (Plan!=nil) | `engine.go` | Add Plan-nil test |

### Phase 2: Deploy checker strengthening (TDD)

| # | Task | Files | Tests |
|---|------|-------|-------|
| 6 | Merge VerifyAll into checkDeploy, wire fetcher+httpClient | `workflow_checks.go` | Update deploy checker tests with health scenarios |
| 7 | Remove checkVerify function + case | `workflow_checks.go` | Remove verify checker tests |
| 8 | Add nil close checker (or no case) | `workflow_checks.go` | Test close step completion with nil checker |

### Phase 3: Transition and gates (TDD)

| # | Task | Files | Tests |
|---|------|-------|-------|
| 9 | Redesign BuildTransitionMessage (4 mode variants) | `bootstrap_guide_assembly.go` | Test all 4 mode variants |
| 10 | Add deploy strategy check + inline selection | `workflow.go` (handleDeployStart) | Test: no strategy → selection guidance (not error); strategy set → proceed |
| 11 | Add CI/CD strategy gate | `workflow.go` (handleCICDStart) | Test cicd blocked without ci-cd strategy |
| 12 | Add router strategy offering | `router.go` | Test p0 offering when strategy empty |

### Phase 4: Cleanup

| # | Task | Files |
|---|------|-------|
| 13 | Delete strategy checker | `workflow_checks_strategy.go` (DELETE), `workflow_checks.go` (remove case) |
| 14 | Delete strategy checker tests | `workflow_checks_strategy_test.go` (DELETE) |
| 15 | Update bootstrap.md content | `internal/content/workflows/bootstrap.md` (remove strategy section, add phase markers to deploy) |

### Files summary

| File | Action |
|------|--------|
| `internal/workflow/bootstrap_steps.go` | Replace StepVerify+StepStrategy with StepClose |
| `internal/workflow/bootstrap.go` | Update constants, skip guards (add close), iteration reset (2-3) |
| `internal/workflow/bootstrap_guide_assembly.go` | Redesign BuildTransitionMessage (4 mode variants) |
| `internal/workflow/engine.go` | Add Plan!=nil check |
| `internal/workflow/validate.go` | Allow len(targets)==0 |
| `internal/workflow/router.go` | Add p0 "set strategy" offering |
| `internal/tools/workflow.go` | Add strategy gates in handleDeployStart + handleCICDStart |
| `internal/tools/workflow_checks.go` | Merge VerifyAll into checkDeploy, remove checkVerify+checkStrategy cases |
| `internal/tools/workflow_checks_strategy.go` | DELETE |
| `internal/tools/workflow_checks_strategy_test.go` | DELETE |
| `internal/content/workflows/bootstrap.md` | Remove strategy section, restructure deploy with phases |

### What stays unchanged

- `action="strategy"` tool handler — standalone, no session needed
- `handleStrategy()` in `workflow_strategy.go` — reads/writes ServiceMeta directly
- Strategy validation (`validStrategies` map) — validates push-dev/ci-cd/manual
- `StrategyToSection` map — drives deploy guidance extraction
- CI/CD workflow — separate 3-step workflow, unchanged
- ServiceMeta struct — `DeployStrategy` field stays
- Deploy workflow — unchanged (reads metas, builds targets)
- `ops.VerifyAll()` — unchanged (called from deploy checker now)

---

## 10. Design Decisions Log

| # | Decision | Why | Alternative | Why rejected |
|---|----------|-----|-------------|--------------|
| 1 | Remove strategy from bootstrap | ZCP-only, auto-assign for dev/simple | Keep as step 6 | Delays bootstrap |
| 2 | Merge verify into deploy | Eliminate redundant re-check | Keep separate | Iteration works merged |
| 3 | Flat steps, not sub-steps | Engine simplicity | Nested sub-steps | Guidance phases suffice |
| 4 | Close as pure trigger (no direct writes) | Avoids double-write with writeBootstrapOutputs | Close writes metas | engine.go:173 already triggers on !Active |
| 5 | Deploy gate: conversational (ask, don't block) | User should decide, not be blocked | Hard error | Bad UX — user wants to deploy, gets error instead of help |
| 6 | CI/CD gate: hard block | CI/CD requires conscious choice | Auto-allow | Would bypass intent |
| 7 | Close skip guard in Phase 1 | Coupled with step replacement | Defer to Phase 4 | Breaks managed-only path |
| 8 | Managed-only close: SKIP | No runtime → no metas → no strategy | Write managed metas | Managed are API-authoritative |

---

## 11. Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Existing tests break (step count) | HIGH | LOW | TDD: write failing tests first |
| Deploy checker slower (VerifyAll) | MEDIUM | LOW | Already happens at verify step today |
| Agent confused by missing verify | LOW | LOW | Guidance phases clarify scope |
| Pre-existing metas blocked | HIGH | LOW | Dev phase — no production services to break |
| Iteration reset range change | LOW | LOW | Explicit 2-3, close excluded |
