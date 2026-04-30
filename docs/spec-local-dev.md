# ZCP Local Development Mode Specification

> **Status**: Authoritative — all local mode code, content, and improvements MUST conform to this document.
> **Scope**: Local mode only. Container mode specs live in `spec-workflows.md`.
> **Date**: 2026-04-22 — Release B refresh (typed enums, dropped
> Environment field, removed hostname-suffix heuristic, removed
> `invertLocalHostname` hack, removed `includeGit` knob).

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

Under plan phase A.4, local env always writes exactly one ServiceMeta
(eagerly at `server.New` time) representing the project itself, keyed by
the Zerops project name. Mode distinguishes whether a Zerops runtime is
linked:

### Local-Stage (runtime linked)

User develops locally; one Zerops runtime is linked as the stage deploy
target. Auto-adopt picks this shape when exactly one runtime exists in
the project at first run.

| Component | Location | Notes |
|-----------|----------|-------|
| Dev server | User's machine | Hot reload, localhost |
| Stage runtime | Zerops | Linked via `ServiceMeta.StageHostname` |
| Managed services | Zerops | Accessible via `zcli vpn up` |

ServiceMeta:
```
{
  "hostname":       "<project-name>",
  "stageHostname":  "<runtime-hostname>",
  "mode":           "local-stage",
  "bootstrapSession": ""   // adopted, not bootstrapped
}
```

If the runtime was already ACTIVE when auto-adopt ran, FirstDeployedAt
is stamped — the fizzy-export case where a service was deployed before
ZCP was aware of it.

### Local-Only (no Zerops runtime linked)

User develops locally; zero or multiple Zerops runtimes exist but none
is linked as stage. Multiple-runtime adoptions land here too — ZCP
refuses to guess which one is stage and asks the user via the
`adopt-local` subaction.

ServiceMeta:
```
{
  "hostname":         "<project-name>",
  "stageHostname":    "",
  "mode":             "local-only",
  "closeDeployMode":  "manual",   // forced on local-only (no push target)
  "bootstrapSession": ""
}
```

Managed services (databases, caches, storage) are NOT given their own
ServiceMeta; their state stays API-authoritative. The local-only meta
is enough to signal "ZCP knows this project" to the router.

### Resolving ambiguity

When multiple runtimes exist, the user picks which one to link as
stage:

```
zerops_workflow action="adopt-local" targetService="<runtime-hostname>"
```

The handler upgrades `Mode` from `local-only` to `local-stage`, sets
`StageHostname`, and (if the runtime is ACTIVE) stamps
`FirstDeployedAt`. Container env does not use this subaction — container
adoption happens through the explicit bootstrap workflow.

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
    Strategy      string  // "" (default zcli push) | "git-push"
}
```

Under Release B the `includeGit` knob was dropped from the tool surface —
local zcli push always runs with `--no-git`. Recipes that need committed
history go through `strategy=git-push` instead, which uses the user's
own git CLI.

### ops.DeployLocal()

1. Validate zcli in PATH (`exec.LookPath`)
2. Resolve targetService hostname → service ID via API
3. Validate zerops.yaml exists at workingDir
4. Run `ValidateZeropsYml(workingDir, targetService)` with local path
5. `zcli login <token>`
6. `zcli push --service-id <id> --project-id <pid> --working-dir <path> --no-git`
7. Return `DeployResult{Status: "BUILD_TRIGGERED", Mode: "local"}`

### Build Polling

`pollDeployBuild()` is shared — extracted to `tools/deploy_poll.go`. Works identically for both modes. `sshDeployer` can be nil (WaitSSHReady skipped).

---

## 6. ServiceMeta

```go
type ServiceMeta struct {
    Hostname                 string           `json:"hostname"`
    Mode                     Mode             `json:"mode,omitempty"`
    StageHostname            string           `json:"stageHostname,omitempty"`
    CloseDeployMode          CloseDeployMode  `json:"closeDeployMode,omitempty"`           // unset | auto | git-push | manual
    CloseDeployModeConfirmed bool             `json:"closeDeployModeConfirmed,omitempty"`
    GitPushState             GitPushState     `json:"gitPushState,omitempty"`              // unconfigured | configured | broken | unknown
    RemoteURL                string           `json:"remoteUrl,omitempty"`                 // git remote (set when GitPushState=configured)
    BuildIntegration         BuildIntegration `json:"buildIntegration,omitempty"`          // none | webhook | actions
    BootstrapSession         string           `json:"bootstrapSession"`
    BootstrappedAt           string           `json:"bootstrappedAt"`
    FirstDeployedAt          string           `json:"firstDeployedAt,omitempty"`           // stamped on successful deploy or adoption at ACTIVE
}
```

The axis-bearing fields (`Mode`, `CloseDeployMode`, `GitPushState`,
`BuildIntegration`) are typed Go enums rather than bare strings — the
vocabulary is shared with plan input and envelope assembly, so the type
system catches drift at compile time. The three deploy-config dimensions
are orthogonal: a service can carry `CloseDeployMode=auto` while
`GitPushState=configured` (push capability provisioned but close still
uses default deploy). `Environment` is not persisted: env is a property
of the currently running ZCP process (runtime-detected via
`runtime.Info.InContainer`), not of a service, and storing it per meta
created a drift vector.

### Local Mode Differences

| Field | Container | Local |
|-------|-----------|-------|
| Hostname | Zerops service hostname | **Zerops project name** |
| StageHostname | Paired stage (standard mode, explicit `ExplicitStage`) | Linked Zerops stage (local-stage only) |
| Mode | dev / standard / simple | **local-stage / local-only** |

### filterStaleMetas Compatibility

Local metas use the project name as `Hostname`, which is never a
live Zerops service hostname. `router.go:filterStaleMetas` therefore
keeps all local-* metas unconditionally:

```go
if m.Mode == PlanModeLocalStage || m.Mode == PlanModeLocalOnly {
    result = append(result, m)  // project-keyed, not service-keyed
    continue
}
```

Stage linkage for local-stage metas is validated separately via
`StageHostname` against the live service list.

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

**A. Bootstrap guidance** (atom pipeline — Option A, infra-only):
- Atoms tagged `environments: [local]` fire on local-mode bootstrap.
- Active local atoms: `bootstrap-discover-local` (discover step) and
  `bootstrap-provision-local` (provision step). Both are route-agnostic
  — the `routes: [classic]` filter was removed so recipe and adopt
  paths also pick them up on local environments.
- Bootstrap under Option A does NOT generate code or deploy — those
  atoms were deleted during the Option A migration. Local-specific
  code-and-deploy guidance moved to the develop workflow (see path B).

**B. Develop workflow guidance** (`buildPrepareGuide`, `buildDeployGuide`):
- `buildPrepareGuide` has env with container/local branches.
- `buildDeployGuide` has env parameter — uses `writeLocalWorkflow` for
  single-target flow.
- First-deploy branch atoms (`deployStates: [never-deployed]`) scaffold
  `zerops.yaml` + write application code + run the first deploy.
- Local key facts: VPN survives deploys, code unchanged locally, zcli
  push semantics.

---

## 10. Health Verification

### Local Dev Server

Agent checks via Bash: `curl -s localhost:{port}/health`

Port is substituted by the agent from `zerops.yaml` (`run.ports`) — no ZCP-side config file. Guidance templates emit the `{port}` placeholder; the agent resolves it and uses the existing Bash tool.

### Zerops Services

Same as container mode — `zerops_verify` uses API + subdomain HTTP.

---

## 11. Close-Mode + Capability + Build Integration

`zerops_deploy` accepts `strategy=""` (default) or `strategy="git-push"`
on the wire — these are wire-vocabulary mechanism selectors, not the
ServiceMeta close-mode. The on-disk model uses three orthogonal fields:

| Field | Wire vocabulary | Container | Local |
|-------|-----------------|-----------|-------|
| `CloseDeployMode=auto` | default deploy via `zerops_deploy` | SSH into dev → `zcli push` from `/var/www` | `zcli push` from user's CWD → linked stage |
| `CloseDeployMode=git-push` | `zerops_deploy strategy="git-push"` | `git push` via GIT_TOKEN + .netrc (tool-managed) | `git push` via user's local git credentials (SSH keys, credential manager) |
| `CloseDeployMode=manual` | (not a deploy mechanism — declares "ZCP stays out of close") | ZCP records evidence; user owns the deploy | same |

`manual` is a ServiceMeta declaration only — passing `strategy="manual"`
as a `zerops_deploy` param is invalid (the tool refuses with a message
explaining the semantic).

### Git-push capability is a separate axis

When a service should push to an external git remote, capability setup
is independent of close-mode. `GitPushState` tracks whether the
capability is provisioned, and `BuildIntegration` (none/webhook/actions)
chooses what fires after the push lands.

| BuildIntegration | What happens after push | Setup delivered by |
|------------------|-------------------------|--------------------|
| `none` | Push lands at the remote; ZCP doesn't track external CI/CD | (no atom — steady state) |
| `webhook` | Zerops dashboard OAuths the repo; webhook fires build | `setup-build-integration-webhook` atom |
| `actions` | GitHub Actions workflow runs `zcli push` back to Zerops | `setup-build-integration-actions` atom |

`GitPushState` + `RemoteURL` are persisted on ServiceMeta and updated by
`action="git-push-setup"`. `BuildIntegration` is persisted on ServiceMeta
and updated by `action="build-integration"` (which refuses unless
`GitPushState=configured`). The two are independent of close-mode — a
service can hold `GitPushState=configured` while keeping
`CloseDeployMode=auto`, and switching to `CloseDeployMode=git-push`
later doesn't require re-setup.

### local-only + default-deploy mechanism is blocked

`local-only` means no Zerops runtime is linked. The default deploy
mechanism (zcli push, `AttemptInfo.Strategy="zcli"`) needs a deploy
target, so the close-mode handler and `zerops_deploy` both refuse
`closeMode=auto` on local-only services. The error points at
`action="adopt-local"` (to link a runtime) or
`zerops_deploy strategy="git-push"` (which doesn't need a stage).

---

## 12. State Directory

```
~/projects/myapp/
  ├── .mcp.json                        ← ZCP_API_KEY (project-scoped token)
  ├── .zcp/
  │   └── state/
  │       ├── sessions/                ← ephemeral workflow sessions
  │       ├── services/appstage.json   ← ServiceMeta (persistent)
  │       └── registry.json            ← session registry
  ├── .env                             ← generated env vars
  ├── .gitignore                       ← includes .env
  ├── zerops.yaml                      ← build/deploy config
  └── (user's code)
```

`.zcp/state/` always at CWD where Claude is opened (`server.go`).

---

## 13. Decision Log

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

## 14. Unverified / Future

| Item | Status |
|------|--------|
| S3 apiUrl accessible without VPN | Needs E2E verification |
| `zcp init vpn` (sudoers setup) | Deferred to Phase 3 |
| Windows VPN auto-connect | Future |
| Docker-Compose local dev | User manages compose, ZCP provides .env |
| Monorepo with single root zerops.yaml + --setup flag | Future enhancement |
