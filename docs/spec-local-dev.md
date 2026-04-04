# ZCP Local Development Mode Specification

> **Status**: Authoritative — all local mode code, content, and improvements MUST conform to this document.
> **Scope**: Local mode only. Container mode is specified in `spec-bootstrap-deploy.md`.
> **Date**: 2026-03-27

---

## 1. Philosophy

User's machine IS the development environment. ZCP provides:
- **Data**: env var discovery, credential bridge, service topology
- **Verification**: local health check guidance, Zerops service verification
- **Deploy**: one-command push from local to Zerops via zcli

ZCP does NOT:
- Manage the user's dev server process (too many variants: Valet, IDE, Docker, CLI)
- Create dev services on Zerops (user's machine replaces the dev service)
- Mount remote filesystems (no SSHFS in local mode)

---

## 2. Environment Detection

```go
// workflow/environment.go
type Environment string
const (
    EnvContainer Environment = "container"  // zcpx on Zerops
    EnvLocal     Environment = "local"      // user's machine
)
func DetectEnvironment(rt runtime.Info) Environment
```

Detection: `runtime.Info.InContainer` is true on Zerops containers. Local machines return `EnvLocal`.

Environment propagation: `Engine.environment` field, set at `NewEngine()`, passed to guidance assembly, bootstrap outputs, and deploy guidance.

---

## 3. Authentication

### Project-Scoped Token

ZCP requires a "Custom access per project" integration token scoped to exactly one project.

```json
// .mcp.json
{
  "mcpServers": {
    "zcp": {
      "command": "zcp",
      "env": { "ZCP_API_KEY": "<project-scoped-token>" }
    }
  }
}
```

Zerops supports three token scope levels:
1. **Full access** — all projects (rejected by ZCP: `ListProjects` returns multiple)
2. **Read access** — read-only (insufficient for deploy)
3. **Custom per-project** — required by ZCP: full access to exactly one project

### Multi-Project Isolation

Each project directory has its own `.mcp.json` + `.zcp/state/`. No collision:
```
~/projects/app1/.mcp.json → token A → project A
~/projects/app2/.mcp.json → token B → project B
```

---

## 4. Topology

### Local Standard (default)

User develops locally, pushes to stage service on Zerops.

| Component | Location | Notes |
|-----------|----------|-------|
| Dev server | User's machine | Hot reload, localhost |
| Stage service | Zerops | appstage — real start, healthCheck |
| Managed services | Zerops | DB, cache, storage — accessible via VPN |

Plan: `{devHostname: "appdev", type: "nodejs@22"}` (unchanged from container mode).
For non-`dev` hostnames, provide explicit stage: `{devHostname: "zmon", type: "go@1", stageHostname: "zmonstage"}`.
Engine: creates **only stage + managed** (skips dev creation in local mode).
ServiceMeta: `{hostname: "appstage", stageHostname: "", environment: "local"}`.

### Local Simple

Single deploy target, no dev/stage separation.

ServiceMeta: `{hostname: "app", mode: "simple", environment: "local"}`.

### Managed-Only

Only databases/caches/storage on Zerops. No runtime targets, no ServiceMeta.

---

## 5. Deploy Architecture

### Conditional Registration

`server.go` routes by environment — same tool name, different schema:

```go
if s.sshDeployer != nil {
    tools.RegisterDeploySSH(...)   // SSH mode: sourceService, workingDir=/var/www
} else {
    tools.RegisterDeployLocal(...) // Local mode: no sourceService, workingDir=CWD
}
```

Both register `zerops_deploy`. Agent always calls the same tool name.

### DeployLocalInput (no sourceService)

```go
type DeployLocalInput struct {
    TargetService string  // Zerops service hostname
    WorkingDir    string  // Local path (default: ".")
    IncludeGit    bool    // -g flag for zcli push
}
```

### ops.DeployLocal()

1. Validate zcli in PATH (`exec.LookPath`)
2. Resolve targetService hostname → service ID via API
3. Validate zerops.yaml exists at workingDir
4. Run `ValidateZeropsYml(workingDir, targetService)` with local path
5. `zcli login <token>`
6. `zcli push <hostname> --working-dir <path> [-g]` (positional arg = hostname)
7. Return `DeployResult{Status: "BUILD_TRIGGERED", Mode: "local"}`

### Build Polling

`pollDeployBuild()` is shared — extracted to `tools/deploy_poll.go`. Works identically for both modes. `sshDeployer` can be nil (WaitSSHReady skipped).

---

## 6. ServiceMeta

```go
type ServiceMeta struct {
    Hostname         string `json:"hostname"`
    Mode             string `json:"mode,omitempty"`
    StageHostname    string `json:"stageHostname,omitempty"`
    DeployStrategy   string `json:"deployStrategy,omitempty"`
    Environment      string `json:"environment,omitempty"`   // "container" or "local"
    BootstrapSession string `json:"bootstrapSession"`
    BootstrappedAt   string `json:"bootstrappedAt"`
}
```

### Local Mode Differences

| Field | Container | Local |
|-------|-----------|-------|
| Hostname | appdev | **appstage** (dev doesn't exist) |
| StageHostname | appstage | **""** (no dev/stage pair) |
| Environment | container | local |

### Strategy Key Separation

`writeBootstrapOutputs` uses `devHostname` for strategy lookup (agent stores strategies under DevHostname), `metaHostname` for ServiceMeta:

```go
devHostname := target.Runtime.DevHostname
strategy := state.Bootstrap.Strategies[devHostname]  // lookup by DevHostname ALWAYS

metaHostname := devHostname
if e.environment == EnvLocal && stageHostname != "" {
    metaHostname = stageHostname
    stageHostname = ""
}
```

### filterStaleMetas Compatibility

`router.go:filterStaleMetas` checks `live[m.Hostname]`. In local mode:
- `hostname=appstage` → appstage exists on Zerops → survives filtering
- If `hostname=appdev` were used → appdev doesn't exist → filtered out → system breaks

---

## 7. Env Var Bridge

### Problem

VPN provides network access but NOT env vars (verified). Local app needs actual values.

### Solution: .env Generation

`ops.FormatEnvFile()` generates `.env` content from `zerops_discover` output. The MCP tool for this is `zerops_env action="generate-dotenv" serviceHostname="app"`, which resolves zerops.yaml `envVariables` refs internally:

```
# Generated by ZCP from zerops_discover
# VPN required: zcli vpn up <projectId>
# WARNING: Contains secrets. Do not commit.

# db (postgresql@16)
db_host=db
db_port=5432
db_password=<actual-password>
```

### zerops.yaml vs .env

| File | Purpose | Where | References |
|------|---------|-------|-----------|
| zerops.yaml envVariables | Zerops container runtime | Zerops | `${db_connectionString}` |
| .env | Local dev | User's machine | Actual values |

Both coexist. `${hostname_varName}` in zerops.yaml works regardless of push source (verified).

---

## 8. VPN

### Default: Guidance Only

Agent provides exact command: `zcli vpn up <projectId>`

VPN requires admin/root — agent cannot start it without setup.

### Hostname Resolution

Both `hostname` and `hostname.zerops` work over VPN (live-verified macOS 2026-03-26). DNS search domain handles plain hostname resolution.

### Connection Diagnostics

When local app can't connect to managed service:
1. `zerops_discover service="db"` — is service RUNNING?
2. `nc -zv db 5432 -w 3` — is VPN working?
3. Compare .env vs `zerops_discover includeEnvs=true` — credentials match?

---

## 9. Guidance System

### Two Independent Paths

**A. Bootstrap guidance** (`assembleGuidance` → `ResolveProgressiveGuidance`):
- `buildGuide` receives `env Environment` (activated from dead `_` parameter)
- `GuidanceParams.Env` propagates through chain
- Environment-specific sections: `generate-local`, `deploy-local`
- Local deploy replaces entire SSH deploy section

**B. Deploy workflow guidance** (`buildPrepareGuide`, `buildDeployGuide`):
- `buildPrepareGuide` has env with container/local branches
- `buildDeployGuide` has env parameter — uses `writeLocalWorkflow` for single-target flow
- Local key facts: VPN survives deploys, code unchanged locally, zcli push semantics

---

## 10. Local Config

```go
// workflow/local_config.go
type LocalConfig struct {
    Port    int    `json:"port"`
    EnvFile string `json:"envFile"`
}
```

Persisted at `.zcp/state/local.json`. Created during bootstrap generate step. Port from zerops.yaml `run.ports`. Used by guidance for localhost health check hint.

---

## 11. Health Verification

### Local Dev Server

Agent checks via Bash: `curl -s localhost:{port}/health`

Port from `LocalConfig` or zerops.yaml. No ZCP code change — agent uses existing Bash tool.

### Zerops Services

Same as container mode — `zerops_verify` uses API + subdomain HTTP.

---

## 12. Deploy Strategies

| Strategy | Container | Local |
|----------|-----------|-------|
| push-dev | SSH: dev → stage | zcli push: local → target |
| push-git | Git push + optional CI/CD | Git push + optional CI/CD (same) |
| manual | zerops_deploy directly | zerops_deploy directly (same) |

Strategy names, selection flow, ServiceMeta storage — all unchanged.

---

## 13. State Directory

```
~/projects/myapp/
  ├── .mcp.json                        ← ZCP_API_KEY (project-scoped token)
  ├── .zcp/
  │   └── state/
  │       ├── sessions/                ← ephemeral workflow sessions
  │       ├── services/appstage.json   ← ServiceMeta (persistent)
  │       ├── registry.json            ← session registry
  │       └── local.json               ← local dev config
  ├── .env                             ← generated env vars
  ├── .gitignore                       ← includes .env
  ├── zerops.yaml                      ← build/deploy config
  └── (user's code)
```

`.zcp/state/` always at CWD where Claude is opened (`server.go`).

---

## 14. Decision Log

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | No dev service on Zerops in local mode | User's machine IS dev |
| D2 | Project-scoped token required | Scoping via token, not env var |
| D3 | Conditional registration — same tool name, different schema | No phantom params, no LLM confusion |
| D4 | hostname=appstage in local ServiceMeta | Must exist on Zerops for filterStaleMetas |
| D5 | Strategy lookup by DevHostname, meta hostname by StageHostname | Strategies map keyed by DevHostname (plan format unchanged) |
| D6 | zcli push positional arg (hostname) | Simpler than --service-id flag |
| D7 | .env generation from zerops_discover | Universal format, VPN-only access |
| D8 | VPN: guidance + diagnostics, optional auto-connect | Admin privileges required |
| D9 | Plan format unchanged — engine adapts per environment | Same agent behavior, different engine routing |

---

## 15. Unverified / Future

| Item | Status |
|------|--------|
| S3 apiUrl accessible without VPN | Needs E2E verification |
| `zcp init vpn` (sudoers setup) | Deferred to Phase 3 |
| Windows VPN auto-connect | Future |
| Docker-Compose local dev | User manages compose, ZCP provides .env |
| Monorepo with single root zerops.yaml + --setup flag | Future enhancement |
