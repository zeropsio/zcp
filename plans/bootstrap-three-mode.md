# Bootstrap Three-Mode Design — Complete Implementation Plan

## Context

Bootstrap currently has two modes (standard and simple) implemented via a `Simple bool` field on `RuntimeTarget`. This creates several problems:

1. **No dev-only mode** — users who want a single dev service for prototyping must either use standard (creates unnecessary stage) or simple (no SSH iteration, no idle start). A "dev" mode would give the SSH iteration workflow without the stage service.

2. **Routing conflict** — `planMode()` (bootstrap.go:314) classifies by `Simple` bool, but `ResolveProgressiveGuidance()` (bootstrap_guidance.go:55) classifies by runtime type via `hasNonImplicitWebserverRuntime()`. These are orthogonal dimensions that contradict: a `Simple=true, Type=bun@1.2` target gets planMode="simple" but guidance="deploy-standard" (bun is non-implicit-webserver).

3. **Progressive guidance is orphaned** — `ResolveProgressiveGuidance()` exists and is tested but never called from the engine. `resolveGuideWithGating()` (bootstrap.go:261) calls `ResolveGuidance(stepName)` (returns raw section) instead.

4. **Phase gates not mode-aware** — G4 requires `stage_verify` evidence even for plans without stage services. A simple or dev-mode bootstrap would block at G4.

5. **Deploy-simple content is a stub** — 6 procedural lines vs ~400 lines for standard mode.

6. **`forceGuide=true` ghost** — referenced in `BuildIterationDelta` output (bootstrap_guidance.go:109) but doesn't exist anywhere as a parameter.

7. **BriefingFor is a single string** — multi-runtime plans (e.g., bun + go) get a concatenated key like `"bun@1.2+go@1"`. If a second briefing is loaded for a different runtime, the old key is overwritten instead of accumulating.

**Intended outcome**: A clean three-mode system where Mode is the primary routing dimension, guidance delivery is wired into the engine, gates adapt to mode, and content is sufficient for all three modes.

---

## Three-Mode Specification

| | **standard** | **dev** | **simple** |
|---|---|---|---|
| Services created | `{name}dev` + `{name}stage` | `{name}dev` only | `{name}` (any hostname) |
| Hostname constraint | must end in "dev" | must end in "dev" | any valid hostname |
| Start command (dev) | `zsc noop --silent` (idle) | `zsc noop --silent` (idle) | real command |
| Start command (stage) | real command + healthCheck | — | — |
| SSH iteration | yes (kill-then-start cycle) | yes (kill-then-start cycle) | pre-first-deploy quick-test only |
| HealthCheck | stage only | no | yes |
| Stage | auto-created, cross-deploy | deferred (promote = new session) | none |
| Phase gates | G0-G4 (full) | G0-G3 (skip G4/stage_verify) | G0-G3 (skip G4/stage_verify) |

**Dev mode** = identical to standard's dev service lifecycle. No stage creation, no stage deploy, no stage verify. Hostname must end in "dev" so future promotion (via new bootstrap session with standard mode) works naturally.

**Simple mode iteration**: For non-implicit-webserver runtimes, SSH quick-test before first deploy (container has no healthCheck yet, so test manually). After first deploy, iterate via redeploy (healthCheck is active, `zerops_verify` drives the loop). Implicit-webserver runtimes skip quick-test entirely.

---

## File-by-File Changes

### 1. `internal/workflow/validate.go`

**Current** (line 31-36):
```go
type RuntimeTarget struct {
    DevHostname string `json:"devHostname"`
    Type        string `json:"type"`
    IsExisting  bool   `json:"isExisting,omitempty"`
    Simple      bool   `json:"simple,omitempty"`
}
```

**Change to**:
```go
type RuntimeTarget struct {
    DevHostname string `json:"devHostname"`
    Type        string `json:"type"`
    IsExisting  bool   `json:"isExisting,omitempty"`
    Mode        string `json:"mode,omitempty"` // "standard" (default), "dev", or "simple"
}
```

**`StageHostname()` method** (line 40-48) — change condition:
```go
func (r RuntimeTarget) StageHostname() string {
    if r.EffectiveMode() != PlanModeStandard {
        return ""
    }
    if base, ok := strings.CutSuffix(r.DevHostname, "dev"); ok {
        return base + "stage"
    }
    return ""
}
```

**Add `EffectiveMode()` method** (after StageHostname):
```go
// EffectiveMode returns the target's mode, defaulting to standard if empty.
func (r RuntimeTarget) EffectiveMode() string {
    if r.Mode == "" {
        return PlanModeStandard
    }
    return r.Mode
}
```

**Validation in `ValidateBootstrapTargets()`** (line 113-143):

After hostname and type validation (line 130), add mode validation:
```go
// Validate and normalize mode.
mode := rt.EffectiveMode()
if mode != PlanModeStandard && mode != PlanModeDev && mode != PlanModeSimple {
    errs = append(errs, fmt.Sprintf("target %q: invalid mode %q (must be standard, dev, or simple)", rt.DevHostname, rt.Mode))
    continue
}
```

Replace the stage hostname block (line 132-143):
```go
// Validate stage hostname based on mode.
switch mode {
case PlanModeStandard:
    stageHostname := rt.StageHostname()
    if stageHostname == "" {
        errs = append(errs, fmt.Sprintf("target %q: dev hostname must end in 'dev' for standard mode", rt.DevHostname))
        continue
    }
    if err := ValidatePlanHostname(stageHostname); err != nil {
        errs = append(errs, fmt.Sprintf("target %q: derived stage hostname %q: %v", rt.DevHostname, stageHostname, err))
        continue
    }
case PlanModeDev:
    if !strings.HasSuffix(rt.DevHostname, "dev") {
        errs = append(errs, fmt.Sprintf("target %q: dev hostname must end in 'dev' for dev mode (enables future promotion to standard)", rt.DevHostname))
        continue
    }
// PlanModeSimple: any valid hostname, no stage — no additional validation.
}
```

**Add mixed-mode validation** (after the target loop, before returning):
```go
// Validate all targets use the same mode.
if len(targets) > 1 {
    firstMode := targets[0].Runtime.EffectiveMode()
    for _, t := range targets[1:] {
        if t.Runtime.EffectiveMode() != firstMode {
            errs = append(errs, fmt.Sprintf("mixed modes not allowed: target %q is %q but first target is %q", t.Runtime.DevHostname, t.Runtime.EffectiveMode(), firstMode))
        }
    }
}
```

### 2. `internal/workflow/bootstrap.go`

**Constants** (line 8-12) — add dev mode:
```go
const (
    PlanModeStandard = "standard"
    PlanModeSimple   = "simple"
    PlanModeDev      = "dev"
)
```

**`planMode()` method** (line 312-324) — rewrite to use Mode field:
```go
func (b *BootstrapState) planMode() string {
    if b.Plan == nil || len(b.Plan.Targets) == 0 {
        return ""
    }
    // All targets have the same mode (enforced by validation).
    return b.Plan.Targets[0].Runtime.EffectiveMode()
}
```

**`resolveGuideWithGating()`** (line 238-269) — wire progressive guidance for deploy step:

Replace line 261 (`guide := ResolveGuidance(stepName)`) with:
```go
var guide string
if stepName == StepDeploy {
    failureCount := 0
    if iteration > 0 {
        failureCount = iteration
    }
    guide = ResolveProgressiveGuidance(stepName, b.Plan, failureCount)
} else {
    guide = ResolveGuidance(stepName)
}
```

### 3. `internal/workflow/bootstrap_guidance.go`

**`ResolveProgressiveGuidance()`** (line 42-81) — rewrite to use mode as primary routing, runtime type as secondary:

```go
func ResolveProgressiveGuidance(step string, plan *ServicePlan, failureCount int) string {
    if step != StepDeploy {
        return ResolveGuidance(step)
    }

    md, err := content.GetWorkflow("bootstrap")
    if err != nil {
        return ""
    }

    var sections []string
    sections = append(sections, extractSection(md, "deploy-overview"))

    mode := PlanModeStandard
    if plan != nil && len(plan.Targets) > 0 {
        mode = plan.Targets[0].Runtime.EffectiveMode()
    }

    switch mode {
    case PlanModeSimple:
        sections = append(sections, extractSection(md, "deploy-simple"))
    case PlanModeDev:
        sections = append(sections, extractSection(md, "deploy-dev"))
        sections = append(sections, extractSection(md, "deploy-iteration"))
    default: // standard
        sections = append(sections, extractSection(md, "deploy-standard"))
        sections = append(sections, extractSection(md, "deploy-iteration"))
    }

    if plan != nil && len(plan.Targets) >= 3 {
        sections = append(sections, extractSection(md, "deploy-agents"))
    }

    if failureCount > 0 {
        sections = append(sections, extractSection(md, "deploy-recovery"))
    }

    var parts []string
    for _, s := range sections {
        if s != "" {
            parts = append(parts, s)
        }
    }
    if len(parts) == 0 {
        return ResolveGuidance(step)
    }
    return strings.Join(parts, "\n\n---\n\n")
}
```

**Remove `hasNonImplicitWebserverRuntime()`** and `implicitWebserverTypes` — no longer needed. Runtime type handling is now inside the content itself (implicit-webserver skip notes remain in the content text).

**`BuildIterationDelta()`** (line 109) — fix forceGuide ghost:

Replace:
```
RECOVERY: Use forceGuide=true to re-fetch full guidance if stuck.
```
With:
```
RECOVERY: Use zerops_workflow action="iterate" to reset and re-fetch full guidance if stuck.
```

### 4. `internal/workflow/bootstrap_evidence.go`

**`writeBootstrapOutputs()`** (line 138-142) — replace Simple bool check:
```go
meta.Mode = target.Runtime.EffectiveMode()
```

(Replaces the `if target.Runtime.Simple { ... } else { ... }` block.)

### 5. `internal/workflow/gates.go`

**Make G4 mode-conditional** — the gate list stays the same but `CheckGate` gets mode awareness.

Add a new function:
```go
// CheckGateForMode checks a phase transition with mode awareness.
// Simple and dev modes skip G4 (stage_verify) since they have no stage.
func CheckGateForMode(from, to Phase, evidenceDir, sessionID, mode string) (*GateResult, error) {
    // Simple and dev modes skip G4 entirely.
    if (mode == PlanModeSimple || mode == PlanModeDev) && from == PhaseVerify && to == PhaseDone {
        return &GateResult{Passed: true, Gate: "G4"}, nil
    }
    return CheckGate(from, to, evidenceDir, sessionID)
}
```

**Update `autoCompleteBootstrap()`** in bootstrap_evidence.go to use `CheckGateForMode`:

In `autoCompleteBootstrap()` (line 66), replace:
```go
result, err := CheckGate(seq[i-1], seq[i], e.evidenceDir, state.SessionID)
```
With:
```go
mode := ""
if state.Bootstrap != nil && state.Bootstrap.Plan != nil && len(state.Bootstrap.Plan.Targets) > 0 {
    mode = state.Bootstrap.Plan.Targets[0].Runtime.EffectiveMode()
}
result, err := CheckGateForMode(seq[i-1], seq[i], e.evidenceDir, state.SessionID, mode)
```

### 6. `internal/workflow/state.go`

**`BriefingFor` → `BriefingsFor`** (line 82):
```go
type ContextDelivery struct {
    GuideSentFor map[string]int `json:"guideSentFor,omitempty"`
    StacksSentAt string         `json:"stacksSentAt,omitempty"`
    ScopeLoaded  bool           `json:"scopeLoaded,omitempty"`
    BriefingsFor []string       `json:"briefingsFor,omitempty"`
}
```

### 7. `internal/tools/knowledge.go`

**`getBriefingFor()`** (line 172-181) — rename and return slice:
```go
func getBriefingsFor(engine *workflow.Engine) []string {
    if engine == nil || !engine.HasActiveSession() {
        return nil
    }
    state, err := engine.GetState()
    if err != nil || state.Bootstrap == nil || state.Bootstrap.Context == nil {
        return nil
    }
    return state.Bootstrap.Context.BriefingsFor
}
```

**Briefing dedup check** (line 114-115) — use `slices.Contains`:
```go
if slices.Contains(getBriefingsFor(engine), briefingKey) {
    return textResult(fmt.Sprintf("[Briefing for %s already loaded. Use query mode for specific topics.]", briefingKey)), nil, nil
}
```

**Briefing recording** (line 134-136) — append instead of overwrite:
```go
_ = engine.UpdateContextDelivery(func(cd *workflow.ContextDelivery) {
    cd.BriefingsFor = append(cd.BriefingsFor, briefingKey)
})
```

Add `"slices"` to the import block.

### 8. `internal/content/workflows/bootstrap.md`

#### 8a. Discover section (line 49-53) — add dev mode option:

Replace lines 49-53:
```markdown
**Environment mode** (ask if not specified):
- **Standard** (default): Creates `{name}dev` + `{name}stage` + shared managed services. NON_HA mode.
- **Dev**: Creates `{name}dev` only + shared managed services. For prototyping — same SSH iteration as standard, no stage. Hostname must end in "dev" for future promotion.
- **Simple**: Creates single `{name}` + managed services. No SSH iteration after first deploy. Only if user explicitly requests it.

Default = standard (dev+stage). If user says "just get it running" or "prototype" → dev mode. If user says "simple" → simple mode.
```

#### 8b. Deploy-simple section (line 819-828) — REWRITE to ~80 lines:

```markdown
<section name="deploy-simple">
### Simple mode — deploy flow

**Prerequisites**: import done, service mounted, env vars discovered, code and zerops.yml written to mount path.

> **Path distinction:** SSHFS mount path `/var/www/{hostname}/` is LOCAL only.
> Inside the container, code lives at `/var/www/`. Never use the mount path as
> `workingDir` in `zerops_deploy` — the default `/var/www` is always correct.

#### zerops.yml for simple mode

Simple mode uses a real `start` command and `healthCheck` — no idle start, no manual SSH lifecycle.

```yaml
zerops:
  - setup: {hostname}
    build:
      base: {runtimeVersion}
      buildCommands: [<from runtime knowledge>]
      deployFiles: [.]   # CRITICAL: self-deploy — MUST be [.] for iteration
      cache: [<runtime-specific cache dirs>]
    run:
      base: {runtimeBase}
      ports:
        - port: {port}
          httpSupport: true
      envVariables:
        # Map discovered variables to app-expected names
      start: {startCommand}   # Real start: ./app, node index.js, bun run src/index.ts
      healthCheck:
        httpGet:
          port: {port}
          path: /health
```

**Key differences from dev+stage template:**
- Single `setup:` entry (not two)
- `start:` uses real command (not idle start)
- `healthCheck` included — Zerops monitors and restarts on failure
- If recipe uses tilde syntax in `deployFiles` (e.g., `.output/~`), adjust `start` to include the directory prefix

#### Quick-test before first deploy (non-implicit-webserver runtimes only)

**Implicit-webserver runtimes (php-nginx, php-apache, nginx, static): skip quick-test — go straight to deploy.** Before first deploy, the container runs bare nginx/apache without zerops.yml config. Endpoint tests return 404.

For all other runtimes, SSH quick-test before the first deploy validates your code works:

1. **Kill any previous process**: `ssh {hostname} "pkill -f '{binary}' 2>/dev/null; fuser -k {port}/tcp 2>/dev/null; true"`
2. **Install deps** (if needed): `ssh {hostname} "cd /var/www && {install_command}"`
3. **Start server** — Bash tool with `run_in_background=true`: `ssh {hostname} "cd /var/www && {start_command}"`
4. **Check startup** — wait 3-5s, then `TaskOutput task_id=... block=false`
5. **Test**: `ssh {hostname} "curl -sf localhost:{port}/health"` | jq .
6. **If broken**: fix code on mount, `TaskStop`, go back to step 1
7. **When working**: proceed to formal deploy

#### Deploy cycle

1. **Deploy**: `zerops_deploy targetService="{hostname}"` — self-deploy. Blocks until complete.
2. **Activate subdomain**: `zerops_subdomain serviceHostname="{hostname}" action="enable"` — MUST call after deploy.
3. **Verify**: `zerops_verify serviceHostname="{hostname}"` — must return status=healthy.

#### Iteration (when verification fails)

If `zerops_verify` returns degraded/unhealthy, iterate (max 3):

1. **Diagnose**: Read `checks` array from verify response + `zerops_logs severity="error" since="5m"`
2. **Fix**: Edit files at mount path — fix zerops.yml, app code, or both
3. **Redeploy**: `zerops_deploy targetService="{hostname}"` — server auto-restarts (healthCheck active)
4. **Re-verify**: `zerops_verify serviceHostname="{hostname}"`

No SSH start needed after redeploy — simple mode has a real `start:` command and `healthCheck`, so the server auto-starts.

After 3 failed iterations: report failure with diagnosis.

#### Recovery patterns

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Build FAILED: "command not found" | Wrong buildCommands | Check recipe, fix build pipeline |
| Build FAILED: "module not found" | Missing dependency init | Add install step to buildCommands |
| App crashes: "EADDRINUSE" | Port conflict | Check run.ports.port matches app |
| App crashes: "connection refused" | Wrong DB/cache host | Check envVariables mapping |
| /status: "db: error" | Missing or wrong env var | Compare envVariables with discovered vars |
| HTTP 502 | Subdomain not activated | Call `zerops_subdomain action="enable"` |
| curl returns empty | Not listening on 0.0.0.0 | Add HOST=0.0.0.0 to envVariables |
| HTTP 500 | App error | Check `zerops_logs` + framework log files |

**Recommendation**: For compiled runtimes (Go, Rust, Java) or complex builds, prefer dev or standard mode — the SSH iteration cycle catches issues faster than redeploy-and-verify.
</section>
```

#### 8c. NEW deploy-dev section (insert after deploy-iteration, before deploy-simple):

```markdown
<section name="deploy-dev">
### Dev-only mode — deploy flow

**Same SSH lifecycle as standard dev. No stage deployment.**

**Prerequisites**: import done, dev mounted, env vars discovered, code written to mount path.

> **Path distinction:** SSHFS mount path `/var/www/{devHostname}/` is LOCAL only.
> Inside the container, code lives at `/var/www/`. Never use the mount path as
> `workingDir` in `zerops_deploy` — the default `/var/www` is always correct.

Dev-only mode uses the same idle start (`zsc noop --silent`) and manual SSH lifecycle as standard dev. The difference: no stage service is created, no stage deploy, no stage verify.

1. **Deploy to dev**: `zerops_deploy targetService="{devHostname}"` — self-deploy. SSHFS mount auto-reconnects.
2. **Start dev** (deploy restarted container — no server runs): kill-then-start via SSH (see Dev iteration cycle). **Implicit-webserver runtimes: skip this step.**
3. **Verify dev**: `zerops_subdomain serviceHostname="{devHostname}" action="enable"` then `zerops_verify serviceHostname="{devHostname}"` — must return status=healthy
4. **Iterate if needed** — if verify returns degraded/unhealthy, enter iteration loop (max 3)
5. **Present URL** to user

**Future promotion**: To add a stage service later, start a new bootstrap session with standard mode using the same `{name}dev` hostname. The existing dev service will be detected as CONFORMANT, and only the stage service will be created.
</section>
```

#### 8d. Service Bootstrap Agent Prompt (line 481-692) — add mode-conditional task table

In the task table (line 596-611), wrap tasks 10-14 with a mode note:

After the existing task table, add:
```markdown
**Mode-specific task scope:**
- **Standard mode**: All tasks 1-14 (dev + stage)
- **Dev mode**: Tasks 1-9 only. After verifying dev, report completion with dev URL. No stage entry, no stage deploy.
- **Simple mode**: Different flow — see deploy-simple section. Single service with real start command, healthCheck, no SSH lifecycle after deploy.
```

### 9. Test Changes

#### 9a. `internal/workflow/validate_test.go`

All `Simple: true` → `Mode: "simple"`:
- Line 61: `TestStageHostname_Simple` — `Mode: "simple"`
- Line 265: `TestValidateBootstrapTargets_SimpleMode_NoStage` — `Mode: "simple"`

Add new test cases:
```go
// TestStageHostname_Dev — Mode=dev returns empty stage hostname
{
    name: "dev_mode",
    target: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", Mode: "dev"},
    want: "",
}

// TestValidateBootstrapTargets_DevMode — requires "dev" suffix, no stage validation
{
    name: "dev_mode_valid",
    targets: []BootstrapTarget{{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", Mode: "dev"}}},
    wantErr: false,
}
{
    name: "dev_mode_no_dev_suffix",
    targets: []BootstrapTarget{{Runtime: RuntimeTarget{DevHostname: "myapp", Type: "bun@1.2", Mode: "dev"}}},
    wantErr: true,
    errContains: "must end in 'dev' for dev mode",
}

// TestValidateBootstrapTargets_ModeDefault — empty mode defaults to standard
{
    name: "empty_mode_defaults_standard",
    targets: []BootstrapTarget{{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}}},
    wantErr: false, // Standard mode, "dev" suffix present → valid
}

// TestValidateBootstrapTargets_InvalidMode
{
    name: "invalid_mode",
    targets: []BootstrapTarget{{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", Mode: "turbo"}}},
    wantErr: true,
    errContains: "invalid mode",
}

// TestValidateBootstrapTargets_MixedModes
{
    name: "mixed_modes_rejected",
    targets: []BootstrapTarget{
        {Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", Mode: "standard"}},
        {Runtime: RuntimeTarget{DevHostname: "api", Type: "go@1", Mode: "simple"}},
    },
    wantErr: true,
    errContains: "mixed modes",
}

// TestEffectiveMode
func TestEffectiveMode(t *testing.T) {
    tests := []struct{ mode, want string }{
        {"", "standard"},
        {"standard", "standard"},
        {"dev", "dev"},
        {"simple", "simple"},
    }
    for _, tt := range tests {
        rt := RuntimeTarget{Mode: tt.mode}
        if got := rt.EffectiveMode(); got != tt.want {
            t.Errorf("EffectiveMode(%q) = %q, want %q", tt.mode, got, tt.want)
        }
    }
}
```

#### 9b. `internal/workflow/bootstrap_test.go`

`TestPlanMode` (line 571-621) — rewrite all cases to use Mode:
```go
{
    name: "nil_plan",
    plan: nil,
    want: "",
},
{
    name: "empty_targets",
    plan: &ServicePlan{},
    want: "",
},
{
    name: "standard_default",
    plan: &ServicePlan{Targets: []BootstrapTarget{
        {Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
    }},
    want: "standard",
},
{
    name: "explicit_standard",
    plan: &ServicePlan{Targets: []BootstrapTarget{
        {Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", Mode: "standard"}},
    }},
    want: "standard",
},
{
    name: "simple",
    plan: &ServicePlan{Targets: []BootstrapTarget{
        {Runtime: RuntimeTarget{DevHostname: "api", Type: "go@1", Mode: "simple"}},
    }},
    want: "simple",
},
{
    name: "dev",
    plan: &ServicePlan{Targets: []BootstrapTarget{
        {Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", Mode: "dev"}},
    }},
    want: "dev",
},
{
    name: "multiple_same_mode",
    plan: &ServicePlan{Targets: []BootstrapTarget{
        {Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", Mode: "dev"}},
        {Runtime: RuntimeTarget{DevHostname: "apidev", Type: "go@1", Mode: "dev"}},
    }},
    want: "dev",
},
```

`TestBuildResponse_DeployStep_UsesProgressiveGuidance` — new test:
```go
// Verify that deploy step returns progressive guidance (not raw ResolveGuidance).
// Check that the detailedGuide contains deploy-overview content.
```

Line 731: `BriefingFor != ""` → `len(BriefingsFor) != 0`

#### 9c. `internal/workflow/bootstrap_guidance_test.go`

All `Simple: true` → `Mode: "simple"` (line 205).

Add new test cases:
```go
// TestResolveProgressiveGuidance_DevMode
{
    name: "dev_mode_gets_deploy_dev_and_iteration",
    step: StepDeploy,
    plan: &ServicePlan{Targets: []BootstrapTarget{
        {Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", Mode: "dev"}},
    }},
    failureCount: 0,
    wantContains: []string{"deploy-dev", "iteration"},
    wantNotContains: []string{"deploy-standard", "deploy-simple"},
}

// TestResolveProgressiveGuidance_SimpleMode
{
    name: "simple_mode_gets_deploy_simple",
    step: StepDeploy,
    plan: &ServicePlan{Targets: []BootstrapTarget{
        {Runtime: RuntimeTarget{DevHostname: "myapp", Type: "bun@1.2", Mode: "simple"}},
    }},
    failureCount: 0,
    wantContains: []string{"deploy-simple", "healthCheck", "quick-test"},
    wantNotContains: []string{"deploy-standard", "deploy-dev"},
}

// TestResolveProgressiveGuidance_StandardBun
{
    name: "standard_bun_gets_deploy_standard",
    step: StepDeploy,
    plan: &ServicePlan{Targets: []BootstrapTarget{
        {Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2"}},
    }},
    failureCount: 0,
    wantContains: []string{"deploy-standard", "iteration"},
    wantNotContains: []string{"deploy-simple", "deploy-dev"},
}
```

Update `TestBuildIterationDelta` — assert no "forceGuide", assert contains "zerops_workflow action=\"iterate\"".

#### 9d. `internal/workflow/bootstrap_evidence_test.go`

Line 106: `Simple: true` → `Mode: "simple"`

Add test verifying `meta.Mode` is set from `EffectiveMode()`:
```go
{
    name: "dev_mode_meta",
    target: BootstrapTarget{Runtime: RuntimeTarget{DevHostname: "appdev", Type: "bun@1.2", Mode: "dev"}},
    wantMeta: ServiceMeta{Mode: "dev", StageHostname: ""},
}
```

#### 9e. `internal/workflow/gates_test.go`

Add tests for `CheckGateForMode`:
```go
func TestCheckGateForMode_SimpleSkipsG4(t *testing.T) {
    // Simple mode: PhaseVerify → PhaseDone should pass without stage_verify evidence.
}

func TestCheckGateForMode_DevSkipsG4(t *testing.T) {
    // Dev mode: PhaseVerify → PhaseDone should pass without stage_verify evidence.
}

func TestCheckGateForMode_StandardRequiresG4(t *testing.T) {
    // Standard mode: PhaseVerify → PhaseDone requires stage_verify evidence.
}
```

#### 9f. `internal/tools/workflow_checks_test.go`

Lines 144, 173, 224, 246, 275, 313, 346: `Simple: true` → `Mode: "simple"` (7 occurrences, mechanical).

#### 9g. `internal/tools/workflow_checks_generate_test.go`

Lines 173, 211, 253, 299, 336: `Simple: true` → `Mode: "simple"` (5 occurrences, mechanical).

#### 9h. `internal/workflow/state_test.go`

Line 143: `BriefingFor string` → `BriefingsFor []string` in serialization test.

Update JSON test fixtures to use `"briefingsFor": [...]` instead of `"briefingFor": "..."`.

#### 9i. `internal/workflow/bootstrap_context_test.go`

Update `TestUpdateContextDelivery_BriefingFor`:
- Rename to `TestUpdateContextDelivery_BriefingsFor`
- Test appending multiple briefings:
```go
cd.BriefingsFor = append(cd.BriefingsFor, "bun@1.2")
// ... later ...
cd.BriefingsFor = append(cd.BriefingsFor, "go@1")
// Assert both present, no duplicates
```

#### 9j. `internal/tools/knowledge_test.go`

Line 356-357: `BriefingFor` → `BriefingsFor` assertions. Use `slices.Contains`.

Add test: two briefings with different runtimes both persist:
```go
// First call: runtime="bun@1.2" → BriefingsFor=["bun@1.2"]
// Second call: runtime="go@1" → BriefingsFor=["bun@1.2", "go@1"]
// Third call: runtime="bun@1.2" → dedup, returns stub
```

---

## Implementation Phases

### Phase 1: Type change + mode routing + gate fix

**TDD order:**
1. **RED**: Update `validate_test.go` — Mode field tests (EffectiveMode, stage hostname per mode, validation per mode, mixed-mode rejection, invalid mode rejection)
2. **RED**: Update `bootstrap_test.go` — planMode tests with three modes using Mode field
3. **RED**: Update `bootstrap_guidance_test.go` — progressive guidance routing per mode (dev → deploy-dev, simple → deploy-simple, standard → deploy-standard)
4. **RED**: Update `gates_test.go` — CheckGateForMode tests (simple/dev skip G4)
5. **RED**: Update all mechanical `Simple: true` → `Mode: "simple"` in workflow_checks tests (12 occurrences)
6. **RED**: Update `bootstrap_evidence_test.go` — Mode field in meta output
7. **GREEN**: Implement:
   - `validate.go`: RuntimeTarget.Mode, EffectiveMode(), validation changes
   - `bootstrap.go`: PlanModeDev constant, planMode() rewrite, progressive guidance wiring
   - `bootstrap_guidance.go`: ResolveProgressiveGuidance() rewrite (mode-based routing), remove hasNonImplicitWebserverRuntime(), fix forceGuide
   - `bootstrap_evidence.go`: EffectiveMode() for meta.Mode, CheckGateForMode in autoCompleteBootstrap
   - `gates.go`: CheckGateForMode()
8. **VERIFY**: `go test ./internal/workflow/... ./internal/tools/... -v`

### Phase 2: Content

1. **RED**: Update guidance tests to assert deploy-simple has "healthCheck", "quick-test", "recovery"
2. **RED**: Add test asserting deploy-dev section exists and contains "dev-only", "iteration"
3. **GREEN**: Rewrite `<section name="deploy-simple">` in bootstrap.md (~80 lines)
4. **GREEN**: Add `<section name="deploy-dev">` in bootstrap.md (~30 lines)
5. **GREEN**: Update discover section with three-mode description
6. **GREEN**: Update agent prompt with mode-conditional task scope note
7. **VERIFY**: `go test ./internal/workflow/... ./internal/content/... -v`

### Phase 3: BriefingsFor multi-runtime

1. **RED**: Update `state_test.go`, `bootstrap_context_test.go`, `knowledge_test.go` for `BriefingsFor []string`
2. **GREEN**: Change `state.go` (field type), `knowledge.go` (getBriefingsFor, slices.Contains, append)
3. **VERIFY**: `go test ./internal/workflow/... ./internal/tools/... -v`

### Final verification
```bash
go test ./... -count=1 -short
make lint-fast
```

---

## Deleted Code

- `hasNonImplicitWebserverRuntime()` function (bootstrap_guidance.go:27-38)
- `implicitWebserverTypes` map (bootstrap_guidance.go:21-23)
- `Simple bool` field on `RuntimeTarget` (validate.go:35) — replaced by `Mode string`

---

## Files Modified (summary)

| File | Changes |
|------|---------|
| `internal/workflow/validate.go` | RuntimeTarget.Mode, EffectiveMode(), validation |
| `internal/workflow/bootstrap.go` | PlanModeDev, planMode() rewrite, progressive guidance wiring |
| `internal/workflow/bootstrap_guidance.go` | Mode-based routing, remove runtime-type routing, fix forceGuide |
| `internal/workflow/bootstrap_evidence.go` | EffectiveMode() for meta, CheckGateForMode in autoComplete |
| `internal/workflow/gates.go` | CheckGateForMode() |
| `internal/workflow/state.go` | BriefingsFor []string |
| `internal/tools/knowledge.go` | getBriefingsFor, slices.Contains, append |
| `internal/content/workflows/bootstrap.md` | deploy-simple rewrite, deploy-dev new, discover update, agent prompt |
| `internal/workflow/validate_test.go` | Mode field tests, new cases |
| `internal/workflow/bootstrap_test.go` | planMode tests, guide test, BriefingsFor |
| `internal/workflow/bootstrap_guidance_test.go` | Mode routing tests |
| `internal/workflow/bootstrap_evidence_test.go` | Mode in meta |
| `internal/workflow/gates_test.go` | CheckGateForMode tests (new file or add to existing) |
| `internal/workflow/state_test.go` | BriefingsFor serialization |
| `internal/workflow/bootstrap_context_test.go` | BriefingsFor persistence |
| `internal/tools/knowledge_test.go` | BriefingsFor dedup |
| `internal/tools/workflow_checks_test.go` | Simple→Mode (7 mechanical) |
| `internal/tools/workflow_checks_generate_test.go` | Simple→Mode (5 mechanical) |
