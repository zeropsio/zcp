# Implementation Plan: Manual Strategy + Post-Bootstrap Orientation

**Date**: 2026-03-23
**Design**: `plans/design-manual-strategy-post-bootstrap.md`
**Branch**: v2
**4 phases, ~260 lines, 12 tests**

---

## Phase 1: Base Instructions Fix

**Goal**: Replace tool-specific SSH list with general principle. Remove hardcoded deploy workflow reference.
**Files**: `internal/server/instructions.go`, `internal/server/instructions_test.go`

### 1.1 RED: Write failing tests

`instructions_test.go` — add to existing test file:

```go
func TestContainerEnvironment_SSHPrinciple(t *testing.T) {
    // Must contain the general principle, not tool-specific list
    tests := []struct {
        name     string
        contains string
    }{
        {"ssh_all_commands", "ALL commands and processes"},
        {"mount_files_only", "reading and writing files"},
        {"rule_principle", "file → mount"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            if !strings.Contains(containerEnvironment, tt.contains) {
                t.Errorf("containerEnvironment should contain %q", tt.contains)
            }
        })
    }
}

func TestContainerEnvironment_NoHardcodedDeployWorkflow(t *testing.T) {
    // Persistence section must NOT hardcode "start a deploy workflow"
    if strings.Contains(containerEnvironment, `action="start" workflow="deploy"`) {
        t.Error("containerEnvironment should not hardcode deploy workflow command")
    }
}
```

### 1.2 GREEN: Rewrite containerEnvironment

`instructions.go` — replace `containerEnvironment` const (lines 15-37):

```go
const containerEnvironment = `

## Your Role

You are the orchestrator. This container is the control plane — it does NOT serve user traffic, run application code, or host databases. Your job is to create, configure, deploy, and manage OTHER services in the project. All user-facing work happens on those services, never on this container.

### Code Access — Two Mechanisms

**SSHFS mount** (` + "`/var/www/{hostname}/`" + `): For reading and writing files only.
Changes appear instantly on the service container. Use Read/Write/Edit tools normally.
IMPORTANT: /var/www/ (no hostname) is THIS container's own filesystem — not a service.

**SSH** (` + "`ssh {hostname} \"command\"`" + `): For ALL commands and processes on services.
Package installs, builds, git operations, server management, debugging — everything that isn't file read/write goes through SSH. Example: ssh appdev "cd /var/www && npm install"
Running installs over SSHFS is orders of magnitude slower — always use SSH.

Rule: If it's a file → mount. If it's a command → SSH.

### Persistence
File edits on mount survive restarts but not deploys (deploy = new container, only deployFiles content persists).
Deploy when: zerops.yml changes, clean rebuild needed, or promote dev → stage.
Code-only changes on dev: just restart the server via SSH — no redeploy needed.

### Deploy = Rebuild
Editing files on mount does NOT trigger deploy. Deploy runs the full build pipeline (buildCommands → deployFiles → start) and creates a new container.

zerops_discover always returns the CURRENT state of all services. Call it whenever you need to refresh your understanding.`
```

### 1.3 Verify

```bash
go test ./internal/server/... -run TestContainerEnvironment -v
```

---

## Phase 2: Manual Strategy Gate

**Goal**: `handleDeployStart()` returns redirect for manual strategy. Router stops offering deploy workflow for manual.
**Files**: `internal/tools/workflow_deploy.go`, `internal/tools/workflow_strategy.go`, `internal/workflow/router.go`, `internal/content/workflows/deploy.md`
**Test files**: `internal/tools/workflow_test.go`, `internal/workflow/router_test.go`

### 2.1 RED: Write failing tests

`workflow_test.go` — add test case for manual strategy redirect:

```go
// In TestWorkflowAction_DeployStart or new test function:
{
    name: "manual_strategy_returns_redirect",
    // Setup: ServiceMeta with DeployStrategy=StrategyManual, BootstrappedAt set
    // Call: action="start" workflow="deploy"
    // Expect: response contains "manual_deploy" action, no session created
    // Expect: response contains zerops_deploy command
    // Expect: no session file in stateDir
}
```

`router_test.go` — update existing "manual strategy" test case (line 78):

```go
{
    name: "manual strategy",
    input: RouterInput{
        ProjectState: StateConformant,
        ServiceMetas: []*ServiceMeta{{
            Hostname: "appdev", BootstrappedAt: "2026-01-01", DeployStrategy: StrategyManual,
        }},
        LiveServices: []string{"appdev"},
    },
    wantWorkflows: []string{"debug", "scale", "configure"}, // NO deploy
    // Currently expects: []string{"deploy", ...} — this will fail (RED)
}
```

### 2.2 GREEN: Implement manual gate

**`workflow_deploy.go`** — add after line 62 (after strategy check, before BuildDeployTargets):

```go
// Manual strategy: return deploy commands directly, no session.
if allManualStrategy(runtimeMetas) {
    targets, mode, _ := workflow.BuildDeployTargets(runtimeMetas)
    if client != nil {
        enrichTargetRuntimeTypes(ctx, client, projectID, targets)
    }
    return jsonResult(buildManualDeployResponse(targets, mode)), nil, nil
}
```

New functions in `workflow_strategy.go` (or new `workflow_manual.go` if file gets long):

```go
// allManualStrategy returns true if all runtime metas have manual strategy.
func allManualStrategy(metas []*workflow.ServiceMeta) bool {
    for _, m := range metas {
        if m.DeployStrategy != workflow.StrategyManual {
            return false
        }
    }
    return true
}

// manualDeployResponse is returned when deploy is called with manual strategy.
type manualDeployResponse struct {
    Action         string                `json:"action"`
    Message        string                `json:"message"`
    Services       []manualServiceInfo   `json:"services"`
    SwitchStrategy string                `json:"switchStrategy"`
}

type manualServiceInfo struct {
    Hostname   string `json:"hostname"`
    Mode       string `json:"mode"`
    Command    string `json:"command"`
    PostDeploy string `json:"postDeploy,omitempty"`
}

func buildManualDeployResponse(targets []workflow.DeployTarget, mode string) manualDeployResponse {
    resp := manualDeployResponse{
        Action:         "manual_deploy",
        Message:        "Deploy strategy is manual. Deploy directly when ready.",
        SwitchStrategy: `zerops_workflow action="strategy" strategies={...}`,
    }
    for _, t := range targets {
        info := manualServiceInfo{
            Hostname: t.Hostname,
            Mode:     t.Role,
            Command:  fmt.Sprintf(`zerops_deploy targetService="%s"`, t.Hostname),
        }
        if t.Role == workflow.DeployRoleDev {
            info.PostDeploy = "New container — start server via SSH, enable subdomain."
        } else if t.Role == workflow.DeployRoleStage {
            info.Command = fmt.Sprintf(`zerops_deploy sourceService="%s" targetService="%s"`,
                findHostname(targets, workflow.DeployRoleDev), t.Hostname)
            info.PostDeploy = "Server auto-starts. Enable subdomain."
        } else {
            info.PostDeploy = "Server auto-starts. Enable subdomain."
        }
        resp.Services = append(resp.Services, info)
    }
    return resp
}
```

**`router.go`** — update `strategyOfferings()` (line 199):

```go
case StrategyManual:
    return nil // No deploy/cicd workflow. Utilities appended by caller.
```

**`workflow_strategy.go`** — update manual description in `buildStrategySelectionResponse()` (lines 134-138):

```go
sb.WriteString("### manual\n")
sb.WriteString("You control when and what to deploy. No guided workflow.\n")
sb.WriteString("- **How it works**: Edit code, call zerops_deploy when ready.\n")
sb.WriteString("- **Good for**: Experienced users, external CI/CD, custom workflows.\n")
sb.WriteString("- **Trade-off**: No guided prepare/verify cycle — you manage the deploy process.\n\n")
```

**`deploy.md`** — rewrite `deploy-manual` section (lines 194-200):

```markdown
<section name="deploy-manual">
### Manual Deploy Strategy

You control when and what to deploy. ZCP does not start a guided workflow for manual strategy.

**Deploy directly:**
- Dev: `zerops_deploy targetService="{devHostname}"`
- Stage from dev: `zerops_deploy sourceService="{devHostname}" targetService="{stageHostname}"`
- Simple: `zerops_deploy targetService="{hostname}"`

**After every deploy:**
- Enable subdomain: `zerops_subdomain action="enable" serviceHostname="..."`
- Verify health: `zerops_verify serviceHostname="..."`

**Dev services (zsc noop):** Server does not auto-start after deploy. Start manually via SSH.
**Stage/simple services:** Server auto-starts with healthCheck.

**Code-only changes (no zerops.yml change):** Edit on mount, restart server via SSH. No redeploy needed.

**Switch to guided deploys:** `zerops_workflow action="strategy" strategies={"hostname":"push-dev"}`
</section>
```

### 2.3 Verify

```bash
go test ./internal/tools/... -run TestWorkflow -v
go test ./internal/workflow/... -run TestRouter -v
```

---

## Phase 3: Post-Bootstrap Orientation

**Goal**: When ServiceMetas exist, `buildProjectSummary()` generates per-service operational guidance in the system prompt.
**Files**: `internal/server/instructions.go` (or new `internal/server/instructions_orientation.go`)
**Test files**: `internal/server/instructions_test.go`

### 3.1 RED: Write failing tests

```go
func TestOrientation_DevMode_ManualStrategy(t *testing.T) {
    // ServiceMeta: mode=dev, strategy=manual, hostname=appdev
    // Live service: appdev, nodejs@22, RUNNING
    // Expect: orientation contains mount path, SSH command pattern, zerops_deploy command
    // Expect: orientation contains "manual" strategy guidance
    // Expect: orientation contains zerops_knowledge pointer
    // Expect: orientation does NOT contain "start workflow deploy"
}

func TestOrientation_StandardMode_DevAndStage(t *testing.T) {
    // ServiceMeta: mode=standard, hostname=appdev, stageHostname=appstage
    // Expect: both dev and stage service blocks
    // Expect: dev has SSH start note, stage has auto-start note
    // Expect: cross-deploy command for stage
}

func TestOrientation_SimpleMode(t *testing.T) {
    // ServiceMeta: mode=simple, hostname=app
    // Expect: auto-start note, healthCheck mention, no SSH management
}

func TestOrientation_ManagedOnly(t *testing.T) {
    // No ServiceMetas (managed-only project)
    // Expect: falls back to current simple list (no orientation)
}

func TestOrientation_NoMetas(t *testing.T) {
    // stateDir empty — no metas at all (pre-bootstrap)
    // Expect: standard project summary, no orientation
}

func TestOrientation_PushDevStrategy(t *testing.T) {
    // ServiceMeta: strategy=push-dev
    // Expect: "Deploy via guided workflow" guidance
    // Expect: zerops_workflow action="start" workflow="deploy" hint
}
```

### 3.2 GREEN: Implement orientation

New function `buildPostBootstrapOrientation()`:

```go
// buildPostBootstrapOrientation generates per-service operational guidance
// when bootstrapped ServiceMetas exist. Returns empty string if no metas.
func buildPostBootstrapOrientation(
    metas []*workflow.ServiceMeta,
    services []platform.ServiceInfo, // from live API
    selfHostname string,
) string
```

**Logic**:
1. Filter metas to complete only (`IsComplete()`)
2. If none → return "" (fallback to standard summary)
3. Build service type map from live API (hostname → type string like "nodejs@22")
4. Build status map from live API (hostname → status like "RUNNING")
5. For each meta, generate service block:
   - Header: `### {hostname} ({type}) — {status}, {mode} mode`
   - Dev services: mount path, SSH command pattern, deploy command, server lifecycle
   - Stage services: cross-deploy command, auto-start note
   - Simple services: deploy command, auto-start note
6. Strategy section (from dominant strategy across metas)
7. Knowledge pointers (runtime-specific `zerops_knowledge` commands)
8. Operations section (debug, configure, scale)

**Integration**: Called from `buildProjectSummary()` after line 206:

```go
metas, _ = workflow.ListServiceMetas(stateDir)

// If bootstrapped metas exist, generate rich orientation
if orientation := buildPostBootstrapOrientation(metas, services, selfHostname); orientation != "" {
    b.WriteString("\n")
    b.WriteString(orientation)
    // Skip standard service list + router (orientation includes both)
    return b.String()
}

// ... existing standard summary code (fallback) ...
```

### 3.3 Verify

```bash
go test ./internal/server/... -run TestOrientation -v
go test ./... -count=1 -short  # full suite
```

---

## Phase 4: Spec + Content Updates

**Goal**: Sync documentation with implementation.
**Files**: `docs/spec-bootstrap-deploy.md`, `internal/content/workflows/deploy.md` (already done in Phase 2)

### 4.1 Changes

**`spec-bootstrap-deploy.md`** — Section 4.2 (Strategy Gate):
- Add Gate 5 (manual redirect) to the gate table
- Update strategy table: manual = "You control deploys. No workflow session."

**`spec-bootstrap-deploy.md`** — Section 4.4 (Guidance Model):
- Add note: manual strategy guidance lives in system instructions, not workflow response

### 4.2 Verify

```bash
go test ./... -count=1 -short  # full suite
make lint-fast                  # lint
```

---

## Execution Order

```
Phase 1 (base instructions)  →  Phase 2 (manual gate)  →  Phase 3 (orientation)  →  Phase 4 (spec)
     ~30 lines                      ~80 lines                  ~150 lines              ~30 lines
     2 tests                        4 tests                    6 tests                 manual review
```

Each phase is independently committable. Phase 1 improves ALL agents immediately (not just manual strategy).

---

## Verification Checklist

After all phases:

- [ ] `go test ./... -count=1 -short` — all pass
- [ ] `make lint-fast` — clean
- [ ] Manual strategy: `action="start" workflow="deploy"` returns redirect, no session
- [ ] Push-dev strategy: deploy workflow works as before (3 steps)
- [ ] CI/CD strategy: cicd workflow works as before
- [ ] Router: manual has no deploy offering, push-dev/ci-cd unchanged
- [ ] System prompt: contains SSH principle, no tool-specific list
- [ ] System prompt (bootstrapped): contains per-service guidance with mount paths, deploy commands
- [ ] System prompt (pre-bootstrap): falls back to standard instructions
