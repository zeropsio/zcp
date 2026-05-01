# C7 diagnosis — subdomain auto-enable gap in run-19

**Status:** root cause identified. Likely a `meta.Mode` empty-string
branch in `serviceEligibleForSubdomain`. Needs code patch verification
before the run-20 dogfood.

## Evidence from run-19

### Workspace import yaml (provision phase, `zerops_import` MCP call)

The workspace yaml the main agent submitted at provision DID set
`enableSubdomainAccess: true` on every dev/stage HTTP runtime:

```yaml
services:
  - hostname: apidev
    type: nodejs@22
    priority: 5
    startWithoutCode: true
    maxContainers: 1
    enableSubdomainAccess: true
    verticalAutoscaling: { minRam: 0.5 }

  - hostname: apistage
    type: nodejs@22
    enableSubdomainAccess: true
    ...
  # appdev / appstage same shape
  # workerdev / workerstage NO enableSubdomainAccess (correct for non-HTTP workers)
```

So `detail.SubdomainAccess` should have been true on apidev/apistage
post-import.

### Deploy result for apidev first deploy

From `runs/19/SESSION_LOGS/subagents/agent-ada4fac1...jsonl`, the
zerops_deploy result:

```json
{
  "status": "DEPLOYED",
  "mode": "ssh",
  "sourceService": "apidev",
  "targetService": "apidev",
  "buildStatus": "ACTIVE",
  "buildDuration": "1m34s",
  "sshReady": true,
  "message": "Successfully deployed to apidev. ..."
  // NO subdomainAccessEnabled, NO subdomainUrl, NO warnings
}
```

`SubdomainAccessEnabled` field is absent. The struct
([`internal/ops/deploy_common.go:38`](../../../internal/ops/deploy_common.go#L38))
declares `omitempty` on the bool, so a `false` value is dropped. No
`warnings` field either. **That's the silent-skip case** —
`maybeAutoEnableSubdomain` returned without setting any field,
without surfacing any warning.

The agent then issued an explicit
`mcp__zerops__zerops_subdomain action=enable` (the run-15 R-15-1
recovery path), which succeeded.

### Call path

`zerops_deploy` for an SSH-mounted dev codebase reaches
[`internal/tools/deploy_ssh.go:219`](../../../internal/tools/deploy_ssh.go#L219)
which calls `maybeAutoEnableSubdomain`. So the function IS invoked
on the recipe-authoring deploy path.

### Predicate trace

`maybeAutoEnableSubdomain` calls
[`serviceEligibleForSubdomain`](../../../internal/tools/deploy_subdomain.go#L150).
The predicate must have returned false for apidev — every other
branch sets `result.SubdomainAccessEnabled = true` (line 71) or
appends a warning (line 67).

For apidev:
1. `meta` — `workflow.FindServiceMeta(stateDir, "apidev")` —
   provision phase records ServiceMeta. The recipe-authoring path
   recorded one: bootstrap_outputs.go has a path that constructs
   `&ServiceMeta{...}`. **Whether `meta.Mode` is populated for
   recipe-authoring scaffolds is the open question.**
2. If `meta != nil && !modeAllowsSubdomain(meta.Mode)`, return false.
3. `modeAllowsSubdomain` switch
   ([`deploy_subdomain.go:107-119`](../../../internal/tools/deploy_subdomain.go#L107))
   returns true only for `PlanModeDev`, `PlanModeStandard`,
   `ModeStage`, `PlanModeSimple`, `PlanModeLocalStage`. Empty string
   falls through `default: return false`.
4. `detail.SubdomainAccess` and `detail.Ports[].HTTPSupport` —
   irrelevant if the mode check already returned false.

### Most likely root cause

**`meta.Mode` is empty (`""`) when the recipe-authoring scaffold's
provision phase records ServiceMeta**, so the mode check at line
156 trips:

```go
if meta != nil && !modeAllowsSubdomain(meta.Mode) {
    return false
}
```

Empty `meta.Mode` → `modeAllowsSubdomain("")` returns false →
predicate returns false → silent skip.

This explains why:
- The predicate's HTTPSupport check at line 173-177 never executes
  (mode check short-circuits earlier).
- Run-15's R-15-1 fix (which the predicate's F8 closure documents)
  works for the F8-specific case (worker codebases with `zsc noop`)
  but doesn't help here because the predicate exits before
  inspecting ports.
- The test
  `TestPlatformEligible_DetailSubdomainAccessFalse_HTTPSupportTrue_Eligible`
  at
  [`deploy_subdomain_test.go:484`](../../../internal/tools/deploy_subdomain_test.go#L484)
  passes — it constructs a fixture without meta, or with meta whose
  Mode IS in the allow-list. The recipe-authoring case
  (meta-with-empty-Mode) is not test-covered.

## Fix candidates

### Option A — populate `meta.Mode` at provision (preferred)

Find the ServiceMeta-construction site in zcprecipator3's provision
flow (likely in `internal/recipe/handlers.go` provision-phase
handler or in `bootstrap_outputs.go`-equivalent for recipe runs)
and populate `Mode` from the workspace yaml's per-service mode hint:

- Codebases with `startWithoutCode: true` + httpSupport → `PlanModeDev`
- Codebases without `startWithoutCode` (cross-deploy stage targets) → `ModeStage`

This aligns the recipe-authoring path with the same Mode taxonomy
the regular deploy tool uses.

### Option B — relax the predicate for empty Mode (safer)

Treat empty `meta.Mode` as "no constraint, fall through to detail
checks":

```go
// internal/tools/deploy_subdomain.go:155-158 (current)
if meta != nil && !modeAllowsSubdomain(meta.Mode) {
    return false
}

// proposed
if meta != nil && meta.Mode != "" && !modeAllowsSubdomain(meta.Mode) {
    return false
}
```

Empty Mode is semantically "unknown", and the live HTTP signal
checks below (SubdomainAccess + Ports HTTPSupport) are
authoritative. An unset Mode shouldn't be MORE restrictive than
missing meta entirely.

### Recommended

**Both.** Option B is the immediate predicate fix (one line, no
provision-side changes). Option A is the longer-term cleanup so
ServiceMeta carries useful state for recipe-authoring contexts.

## Verification

- Add a unit test in `deploy_subdomain_test.go`:
  `TestPlatformEligible_MetaWithEmptyMode_HTTPSupportTrue_Eligible` —
  meta whose Mode is empty + detail with `SubdomainAccess=true` OR
  `Ports[].HTTPSupport=true`. Predicate returns true.
- Re-test the run-19 dogfood scenario: provision phase records
  empty-Mode meta; predicate fires; subdomain auto-enables on first
  deploy; result.SubdomainAccessEnabled = true.

The fix is small. Land it before the run-20 final-gate dogfood.
