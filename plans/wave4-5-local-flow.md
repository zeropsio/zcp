# Wave 4-5: Local Flow Implementation Plan

## Goal

Enable ZCP to work from a local development machine (not inside a Zerops container). The agent develops locally, deploys via `zcli push`, and verifies via API.

---

## What Exists Today

| Component | Container | Local |
|-----------|-----------|-------|
| Environment detection | `runtime.Info.InContainer=true` | `runtime.Info.InContainer=false` |
| `Environment` type | `EnvContainer` | `EnvLocal` (detected but unused) |
| Auth | `ZCP_API_KEY` or zcli `cli.data` fallback | Same — already works |
| Deploy tool | SSH deploy (`zerops_deploy`) | **NOT registered** (sshDeployer=nil) |
| Mount tool | SSHFS (`zerops_mount`) | **NOT registered** (mounter=nil) |
| Bootstrap guidance | Container-only (SSHFS, SSH start) | **Missing** |
| Deploy guidance | Container-only | **Missing** |
| Preflight checks | N/A (container auto-ready) | **Missing** |

---

## Wave 4: Foundation

### 4.1 Preflight Checks

**New file:** `internal/ops/preflight.go` (~80 lines)

```go
type PreflightResult struct {
    ZCLI       bool   `json:"zcli"`       // zcli binary found in PATH
    ZCLIPath   string `json:"zcliPath"`   // resolved path
    Auth       bool   `json:"auth"`       // valid credentials available
    VPN        bool   `json:"vpn"`        // can reach Zerops services (API ping)
    Project    string `json:"project"`    // resolved project ID
}

func CheckPreflight(ctx context.Context, client platform.Client, authInfo *auth.Info) (*PreflightResult, error)
```

Checks:
1. `exec.LookPath("zcli")` — binary available?
2. `authInfo.Token != ""` — credentials resolved?
3. `client.GetUserInfo(ctx)` — API reachable? (doubles as VPN check)
4. `authInfo.ProjectID != ""` — project scoped?

**New file:** `internal/tools/preflight.go` (~60 lines)

Register `zerops_preflight` MCP tool that runs `CheckPreflight` and returns result. Agent calls this before starting local workflows.

### 4.2 Local Deploy Path

**Modify:** `internal/ops/deploy.go`

Current `Deploy()` requires `SSHDeployer`. Add local branch:

```go
func Deploy(ctx context.Context, ..., rtInfo runtime.Info) (*DeployResult, error) {
    // ... validation ...
    if !rtInfo.InContainer {
        return deployLocal(ctx, client, authInfo, targetService, workingDir, includeGit)
    }
    return deploySSH(ctx, sshDeployer, ...)
}
```

**New file:** `internal/ops/deploy_local.go` (~120 lines)

```go
func deployLocal(ctx context.Context, client platform.Client, authInfo *auth.Info,
    targetService, workingDir string, includeGit bool) (*DeployResult, error)
```

Steps:
1. Validate `workingDir` exists and contains `zerops.yml`
2. Run `zcli login --zeropsToken {token}` (auto-login, mirrors SSH path)
3. Run `zcli push {targetService} --projectId {projectID}` from `workingDir`
4. Parse zcli output for build process ID
5. Poll `client.PollBuild()` until ACTIVE or BUILD_FAILED
6. Return `DeployResult` with status + build logs

**Important:** `zcli push` blocks until build completes, so polling may not be needed — check zcli behavior. If it blocks, just capture exit code + output.

**Modify:** `internal/tools/deploy.go`

- Always register deploy tool (remove `if s.sshDeployer != nil` guard)
- Pass `rtInfo` to `ops.Deploy()`
- Tool works in both environments — environment detection happens inside `ops.Deploy()`

**Modify:** `internal/server/server.go`

```go
// OLD:
if s.sshDeployer != nil {
    tools.RegisterDeploy(...)
}

// NEW:
tools.RegisterDeploy(s.server, s.client, projectID, s.sshDeployer, s.authInfo, s.logFetcher, s.rtInfo)
```

### 4.3 Mount Tool — Local No-Op

Local has no SSHFS. Two options:
- **A)** Don't register `zerops_mount` locally (current behavior — mounter=nil)
- **B)** Register with local stub that returns "files are local, no mount needed"

Recommend **A** — guidance should tell agent files are local. No stub needed.

---

## Wave 5: Guidance & Completion

### 5.1 Bootstrap Guidance — Local Sections

**Modify:** `internal/content/workflows/bootstrap.md`

Add local-specific sections alongside container ones:

| Section | Container (exists) | Local (new) |
|---------|-------------------|-------------|
| `generate-common` | SSHFS rules | Local filesystem rules |
| `generate-standard` | noop start, SSH iteration | **Real start**, zcli push iteration |
| `deploy-overview` | SSH deploy, SSHFS | zcli push, local files |
| `deploy-standard` | SSH self-deploy + cross-deploy | zcli push dev + zcli push stage |
| `deploy-iteration` | SSH kill+start cycle | Local kill+start or zcli push |

**Key difference:** In local mode, even dev services use **real start commands** (not `zsc noop`), because the user won't SSH into the container to start the server. The iteration cycle becomes: edit locally → `zcli push` → verify.

**Approach:** Either:
- **A)** Separate section names: `generate-standard-local`, `deploy-standard-local` etc.
- **B)** Conditional blocks within existing sections using environment markers

Recommend **A** — cleaner, easier to test.

### 5.2 Progressive Guidance — Environment Filtering

**Modify:** `internal/workflow/bootstrap_guidance.go`

`ResolveProgressiveGuidance` currently filters by mode. Add environment filtering:

```go
func ResolveProgressiveGuidance(step string, plan *ServicePlan, failureCount int, env Environment) string {
    // ...
    case StepGenerate:
        sections = append(sections, extractSection(md, "generate-common"))
        suffix := ""
        if env == EnvLocal {
            suffix = "-local"
        }
        if modes[PlanModeStandard] {
            sections = append(sections, extractSection(md, "generate-standard"+suffix))
        }
        // ...
}
```

### 5.3 Knowledge Injection — Environment Context

**Modify:** `internal/workflow/bootstrap_guide_assembly.go`

`assembleKnowledge` currently ignores `env` parameter (`_`). For local flow:
- Same runtime/service knowledge (platform rules are identical)
- Different env var delivery (local apps need `.env` or VPN access)
- No SSHFS-specific rules

Most knowledge is environment-independent. The differences are in **guidance**, not knowledge.

### 5.4 Deploy Guidance — Local Sections

**Modify:** `internal/content/workflows/deploy.md`

Add local variants:

```
<section name="deploy-execute-standard-local">
### Standard mode: Dev+Stage local deploy

1. **Deploy to dev**: `zerops_deploy targetService="{devHostname}"` — zcli push from local dir
2. **Verify dev**: `zerops_subdomain action="enable"` + `zerops_verify`
3. **Deploy to stage**: `zerops_deploy targetService="{stageHostname}"` — zcli push
4. **Verify stage**: same
</section>
```

Note: local deploy is simpler — no SSH start needed (real start cmd in zerops.yml).

---

## Mode × Environment Matrix (Complete)

| | Standard Container | Standard Local | Dev Container | Dev Local | Simple Container | Simple Local |
|---|---|---|---|---|---|---|
| **Services** | dev+stage+managed | dev+stage+managed | dev+managed | dev+managed | 1 runtime+managed | 1 runtime+managed |
| **File access** | SSHFS `/var/www/` | Local filesystem | SSHFS | Local | SSHFS | Local |
| **Deploy** | SSH (git+zcli inside) | zcli push from local | SSH | zcli push | SSH | zcli push |
| **Dev start** | `zsc noop` + SSH start | Real start (auto) | Same | Real start | Real start | Real start |
| **Dev healthCheck** | None (agent controls) | Present (auto-restart) | None | Present | Present | Present |
| **Iteration** | Edit mount → SSH restart | Edit local → zcli push | Same | Same | Edit mount → redeploy | Edit local → zcli push |
| **Prerequisites** | Container exists | zcli + VPN + auth | Same | Same | Same | Same |

**Key insight:** Local mode ALWAYS uses real start commands, even for standard/dev. The `zsc noop` pattern exists only in containers where the agent has SSH access to manually start/stop the server.

---

## Files Summary

| File | Action | Est. lines |
|------|--------|-----------|
| `internal/ops/preflight.go` | NEW | ~80 |
| `internal/ops/deploy_local.go` | NEW | ~120 |
| `internal/tools/preflight.go` | NEW | ~60 |
| `internal/ops/deploy.go` | MODIFY — add local branch | +20 |
| `internal/tools/deploy.go` | MODIFY — always register, pass rtInfo | +10 |
| `internal/server/server.go` | MODIFY — always register deploy | +2, -2 |
| `internal/workflow/bootstrap_guidance.go` | MODIFY — env-aware filtering | +30 |
| `internal/workflow/bootstrap_guide_assembly.go` | MODIFY — use env param | +10 |
| `internal/content/workflows/bootstrap.md` | ADD local sections | +200 |
| `internal/content/workflows/deploy.md` | ADD local sections | +80 |
| Tests for all above | NEW | ~300 |
| **Total** | | **~900 new, ~60 modified** |

---

## Implementation Order

1. **Preflight** — `ops/preflight.go` + `tools/preflight.go` + tests
2. **Local deploy** — `ops/deploy_local.go` + modify `deploy.go` + tests
3. **Always register deploy** — `server.go` + `tools/deploy.go`
4. **Guidance sections** — bootstrap.md + deploy.md local sections
5. **Progressive guidance env filter** — `bootstrap_guidance.go`
6. **Integration test** — full local bootstrap flow with mock zcli

## Dependencies

- `zcli` binary must be available on the local machine
- VPN must be active for API access and service connectivity
- `zerops.yml` must be in the working directory
- Auth via `ZCP_API_KEY` or zcli `cli.data` (already implemented)
