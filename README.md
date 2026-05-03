# ZCP — Zerops Control Plane

MCP server that gives an LLM full control over a Zerops project. Runs as a `zcp@1` service inside the project it manages.

## Integration model

```
User ←→ Claude Code (terminal in code-server) ←→ ZCP (MCP over STDIO) ←→ Zerops API
                                                                        ←→ sibling services (SSH/SSHFS over VXLAN)
```

The user opens code-server on the `zcp` service subdomain. Claude Code is preconfigured with ZCP as its MCP server. The user describes what they want, the LLM figures out what to do, calls ZCP tools to make it happen.

ZCP authenticates once at startup (env var or zcli token), discovers which project it's in, and exposes everything as MCP tools. The LLM sees a system prompt with the environment concept, current service classification, and available workflows — the LLM decides what to do.

## What the LLM can do

Through ZCP tools, the LLM can:

- **Bootstrap a full stack** — from "I need a Node.js app with PostgreSQL" to running services with health checks, in one conversation
- **Deploy code** — writes files via SSHFS mount, triggers build pipeline via SSH push
- **Debug** — read logs, check events, verify service health
- **Scale** — adjust CPU, RAM, disk, container count
- **Configure** — manage env vars, subdomains, shared storage connections
- **Monitor** — discover services, check statuses

---

## Architecture

```
cmd/zcp/main.go → internal/server  → MCP tools  → internal/ops       → internal/platform → Zerops API
                                                 → internal/workflow   (envelope → plan → atoms)
                                                 → internal/knowledge  (on-demand guides)
                                                 → internal/auth       (token resolution)
```

| Package | Responsibility |
|---------|---------------|
| `cmd/zcp` | Entrypoint, STDIO server |
| `internal/server` | MCP server setup, tool registration, base instructions |
| `internal/tools` | MCP tool handlers |
| `internal/ops` | Business logic — deploy, verify, import, scale, browser automation |
| `internal/workflow` | Envelope composition, typed Plan dispatch, atom synthesis, bootstrap/develop conductors, work session |
| `internal/content` | Embedded templates (`templates/`) + atom corpus (`atoms/*.md`) |
| `internal/platform` | Zerops API client, types, error codes |
| `internal/auth` | Token resolution (env var / zcli), project discovery |
| `internal/knowledge` | Text search, embedded guides, themed knowledge base |
| `internal/runtime` | Container vs local detection, self-service hostname |
| `internal/schema` | Live Zerops YAML schema fetching, caching, enum extraction |
| `internal/service` | ServiceMeta persistence (mode, close-mode, first-deploy stamp) |
| `internal/init` | Runtime initialization (SSHFS mounts, nginx) |
| `internal/catalog` | API-driven version catalog sync for test validation |
| `internal/sync` | Guide sync (zeropsio/docs pull, GitHub push) |
| `internal/preprocess` | Expression preprocessor for `<@...>` placeholders |

---

## Pipeline

Every workflow-aware tool response runs through four stages:

1. **`ComputeEnvelope`** — reads project state, ServiceMetas, live services, active sessions; produces a typed `StateEnvelope`.
2. **`BuildPlan`** — pure envelope-shape dispatch; returns `Plan{Primary, Secondary, Alternatives}`. Each `NextAction` names a tool + args + rationale.
3. **`Synthesize`** — filters the atom corpus (`internal/content/atoms/*.md`) against envelope axes (phases, modes, strategies, runtimes, environments, routes, steps, deployStates). For byte-equal envelopes the synthesized guidance is byte-identical.
4. **`RenderStatus`** — emits the markdown status block shown to the LLM.

The primary MCP entry is `zerops_workflow action="start" workflow="develop"` — every task that changes code or deploys opens a develop work session. `action="status"` is the canonical recovery call when state is unclear (after compaction or between tasks). `workflow="bootstrap"` creates or adopts infrastructure; `workflow="cicd"` and `workflow="export"` are stateless and return guidance only.

Direct tools (no workflow session): `zerops_discover`, `zerops_logs`, `zerops_events`, `zerops_knowledge`, `zerops_env`, `zerops_manage`, `zerops_scale`, `zerops_subdomain`, `zerops_verify`. Workflow-gated: `zerops_deploy` (needs adopted services), `zerops_mount` and `zerops_import` (need an active workflow session).

---

## Workflows

### Immediate (stateless, no session)

- **cicd** — generates pipeline config guidance.
- **export** — generates export guidance.

### Orchestrated (session-tracked)

- **bootstrap** — 3 steps (`discover` / `provision` / `close`). Infrastructure-only; writes ServiceMetas + zerops.yaml scaffolding for verification. No application code, no first deploy.
- **develop** — per-PID Work Session. Scaffolds first deploy (`deployStates=never-deployed` branch) or edits the loop (`deployStates=deployed` branch). Auto-closes when every scope service has a succeeded deploy + passed verify.

---

## Bootstrap workflow

Bootstrap opens with a two-call discovery+commit: the first call (no `route` argument) returns `routeOptions[]` ranked `resume` > `adopt` > `classic`. The second call supplies `route=<chosen>` and opens the session.

```
┌──────────┐   ┌───────────┐   ┌───────┐
│ DISCOVER │──▶│ PROVISION │──▶│ CLOSE │
└──────────┘   └───────────┘   └───────┘
```

| Step | What happens | Checker |
|------|-------------|---------|
| **discover** | Validate hostnames against live catalog, compute `plan.IsAllExisting()`, check hostname locks | Plan must be valid; adoption targets must exist |
| **provision** | Generate import.yaml, create services via API, mount dev filesystems, discover env vars from managed services | Dev runtime RUNNING/ACTIVE, stage READY_TO_DEPLOY, managed deps RUNNING + env vars |
| **close** | Writes `ServiceMeta` per target; session file deleted; registry entry removed | — |

Provision is single-shot. If the checker fails, the session surfaces the error and escalates to the user; bootstrap does not iterate. Generate + deploy work for application code is owned by the develop workflow's first-deploy branch.

### Plan model

The **discover** step validates a `ServicePlan`:

```
ServicePlan
  └─ Targets[]
       ├─ Runtime
       │    ├─ DevHostname      "appdev"
       │    ├─ Type             "nodejs@22"
       │    ├─ BootstrapMode    "standard" | "dev" | "simple"
       │    └─ StageHostname()  → "appstage" (auto-derived for standard mode)
       └─ Dependencies[]
            ├─ Hostname       "db"
            ├─ Type           "postgresql@16"
            ├─ Mode           "NON_HA" (auto-defaulted)
            └─ Resolution     "CREATE" | "EXISTS" | "SHARED"
```

**Standard mode** (default): runtime gets a dev+stage pair. Dev uses `deployFiles: [.]` and `startWithoutCode: true`; stage builds from committed code (cross-deploy from dev or git-push).

**Dev mode**: single dev service, no stage. For prototyping and quick iterations.

**Simple mode**: single service with real start command + healthCheck.

---

## Develop workflow

Every task that changes code or deploys runs under a per-PID Work Session at `.zcp/state/work/{pid}.json`. The session records intent + services in scope + a capped history of deploy/verify attempts; it does not copy strategy, mode, or service status (those are read fresh from `ServiceMeta` + API on every call).

Two branches, selected by `deployStates`:

- **`never-deployed`** — first-deploy branch atoms scaffold `zerops.yaml`, write application code, run the first deploy, stamp `FirstDeployedAt`. First deploy always uses the default self-deploy mechanism regardless of `ServiceMeta.CloseDeployMode`.
- **`deployed`** — edit-loop branch; per-service close-mode (`auto` / `git-push` / `manual`) drives the atoms that fire at develop close.

Auto-close fires when every scope service has `Deploys[last].Success=true AND Verifies[last].Success=true`. Failure-path iteration tiers (diagnose → systematic → STOP) are surfaced via `BuildIterationDelta`; `defaultMaxIterations=5` caps the session, after which it closes with `CloseReason=iteration-cap`.

---

## ServiceMeta

`ServiceMeta` is the per-service record of how this project uses a given hostname. Stored at `{stateDir}/services/{hostname}.json`, one file per runtime.

| Field | Written by | Meaning |
|---|---|---|
| `Hostname`, `Mode`, `StageHostname` | bootstrap close | Identifies the service and its standard-mode pairing |
| `BootstrappedAt`, `BootstrapSession` | bootstrap close | `BootstrappedAt` stamped on completion; `BootstrapSession` empty marks adoption |
| `CloseDeployMode` (+ `CloseDeployModeConfirmed`) | `zerops_workflow action="close-mode"` | What `action="close"` does: `auto` (zcli push) / `git-push` (commit + push to remote) / `manual` (ZCP yields). Empty until the user opts in. |
| `GitPushState` (+ `RemoteURL`) | `zerops_workflow action="git-push-setup"` | Git-push capability state: `unconfigured` / `configured` / `broken` / `unknown`. Orthogonal to close-mode — `git-push-setup` can land while close-mode stays `auto`. |
| `BuildIntegration` | `zerops_workflow action="build-integration"` | ZCP-managed CI shape consuming git pushes: `none` / `webhook` / `actions`. Requires `GitPushState=configured`. |
| `FirstDeployedAt` | develop first-deploy verify | Stamped once the first deploy has passed verify |

Close-mode + git-push state + build-integration are read fresh from disk on every workflow turn — never cached in session state. The `git-push` deploy strategy gates on the working tree carrying a commit (per spec D2b), not on `FirstDeployedAt`.

---

## Deploy mechanics

ZCP sits on the same VXLAN network as every service in the project. Deploy path:

1. SSHFS mount (container environment) gives filesystem access to the target — code lives at `/var/www/{hostname}/`.
2. LLM writes code + `zerops.yaml` directly to the mount path.
3. `zerops_deploy` SSHes into the target and runs `zcli push`.
4. The Zerops build pipeline picks it up.

Local environment has no SSHFS mount: code lives in the user's working directory, `zerops_deploy` runs `zcli push` against the stage service.

Dev services use `deployFiles: [.]` and `startWithoutCode: true`. Stage services build from committed code. PHP runtimes omit `start:` entirely (implicit-webserver).

## Knowledge system

Platform knowledge comes from two sources baked into the binary plus one fetched at runtime:

- **Atom corpus** (`internal/content/atoms/*.md`) — axis-tagged knowledge synthesized per turn by the workflow pipeline. Never delivered wholesale.
- **Knowledge guides, themes, bases** (`internal/knowledge/`) — pulled on demand via `zerops_knowledge` by topic or axis tuple.
- **Live schemas** — `zerops.yaml` and `import.yaml` JSON schemas fetched from the Zerops API at runtime, cached 24h. Provide authoritative enum values (service types, build bases, run bases, modes, policies) and field descriptions.

### Guide sync

Guide markdown is **gitignored** — pulled from `zeropsio/docs` before build, edits pushed back as PRs.

```bash
zcp sync pull guides                        # All guides from zeropsio/docs (GitHub API)
zcp sync push guides                        # Creates PR on zeropsio/docs
```

No local clones — everything goes through `gh` CLI and the GitHub API. Config: `.sync.yaml`.

## Session persistence

State lives at `.zcp/state/`:

| Path | Purpose |
|------|---------|
| `sessions/{id}.json` | Bootstrap session — route, step, plan, env vars |
| `work/{pid}.json` | Per-PID develop Work Session — intent, services in scope, deploy/verify attempt history |
| `services/{hostname}.json` | ServiceMeta (mode, close-mode + git-push state + build-integration, stage pairing, first-deploy stamp) |
| `registry.json` | Tracks both infrastructure sessions and work sessions; auto-prunes dead PIDs and sessions > 24h old |

Bootstrap sessions survive process restart and can be reattached via `route=resume`. Work sessions are per-process — they die with their PID; code work survives in git/disk.

---

## Development

```bash
go test ./... -count=1 -short    # All tests, fast
go test ./... -count=1 -race     # All tests with race detection
go build -o bin/zcp ./cmd/zcp    # Build
make lint-fast                   # Lint (~3s)
```

E2E tests need a real Zerops project: `go test ./e2e/ -tags e2e` (requires `ZCP_API_KEY` or zcli login).

## Release

```bash
make release        # Minor bump (e.g. v8.104.0 → v8.105.0)
make release-patch  # Patch bump (e.g. v8.104.0 → v8.104.1)
```

Both run tests before tagging. If tests fail, the release is aborted. Requires a clean worktree (no uncommitted changes to tracked files; untracked files are ignored).
