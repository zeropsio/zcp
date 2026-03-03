# ZCP — Zerops Control Plane

MCP server that gives an LLM full control over a Zerops project. Runs as a `zcp@1` service inside the project it manages.

## Integration model

```
User ←→ Claude Code (terminal in code-server) ←→ ZCP (MCP over STDIO) ←→ Zerops API
                                                                        ←→ sibling services (SSH/SSHFS over VXLAN)
```

The user opens code-server on the `zcp` service subdomain. Claude Code is preconfigured with ZCP as its MCP server. The user describes what they want, the LLM figures out what to do, calls ZCP tools to make it happen.

ZCP authenticates once at startup (env var or zcli token), discovers which project it's in, and exposes everything as MCP tools. The LLM sees a system prompt with the current project state and a routing table that tells it which workflow to start.

## What the LLM can do

Through ZCP tools, the LLM can:

- **Bootstrap a full stack** — from "I need a Node.js app with PostgreSQL" to running services with health checks, in one conversation
- **Deploy code** — writes files via SSHFS mount, triggers build pipeline via SSH push
- **Debug** — read logs, check events, verify service health
- **Scale** — adjust CPU, RAM, disk, container count
- **Configure** — manage env vars, subdomains, shared storage connections
- **Monitor** — discover services, check statuses

## How bootstrap works

Bootstrap is the core flow. The LLM gets a user request ("deploy a Go API with Postgres") and ZCP guides it through 11 sequential steps:

1. **detect** — discover existing services, classify project state
2. **plan** — choose runtimes, managed services, hostnames (strict `[a-z0-9]`, dev/stage pairs)
3. **load-knowledge** — fetch platform rules for the chosen stack (binding, ports, env vars, wiring)
4. **generate-import** — write import.yml to create the infrastructure
5. **import-services** — call Zerops API, wait for services to come up
6. **mount-dev** — SSHFS mount dev service filesystems
7. **discover-envs** — read actual env vars Zerops set for managed services (connection strings, ports, credentials)
8. **generate-code** — write zerops.yml + app code using real env vars from step 7
9. **deploy** — SSH push to trigger build pipeline, verify with health checks
10. **verify** — independent verification of all services
11. **report** — present results with URLs

The ordering matters. Code generation happens *after* env var discovery — no hardcoded guesses. The conductor enforces this sequence; the LLM can't skip ahead.

When verification fails, the LLM iterates: read logs, fix code on the mount, redeploy, re-verify. Up to 3 attempts per service.

## Deploy mechanics

ZCP sits on the same VXLAN network as all project services. It deploys via SSH:

1. SSHFS mount gives filesystem access to the target container
2. LLM writes code + zerops.yml directly to the mount path
3. `zerops_deploy` SSHes into the target, initializes git, runs `zcli push`
4. Zerops build pipeline picks it up from there

Dev services get source-deployed (`deployFiles: [.]`). Stage services get proper build output. Dev uses `startWithoutCode: true` so the container is already running before the first deploy.

## Knowledge system

Platform knowledge is compiled into the binary. The LLM queries it before generating any configuration:

- **Briefings** — stack-specific rules (e.g., "Node.js must bind 0.0.0.0, deploy node_modules, use these env var patterns for PostgreSQL wiring")
- **Recipes** — complete framework configs (Laravel, Next.js, Django, etc.)
- **Infrastructure scope** — full import.yml and zerops.yml schema reference
- **Text search** — BM25 search across all embedded docs

This prevents the LLM from guessing Zerops-specific syntax. It reads the rules, then generates config.

## System prompt and routing

At startup, ZCP calls the API to list services and classifies project state:

- **FRESH** (no runtime services) → route to bootstrap
- **CONFORMANT** (dev+stage pairs detected) → route to deploy
- **NON_CONFORMANT** (services exist without dev/stage pattern) → route to bootstrap alongside existing services, never delete without explicit approval

This classification is injected into the system prompt so the LLM knows what to do before its first tool call.

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
make release        # Minor bump (v2.62.0 → v2.63.0)
make release-patch  # Patch bump (v2.62.0 → v2.62.1)
```

Both run tests before tagging. If tests fail, the release is aborted. Requires a clean worktree (no uncommitted changes to tracked files; untracked files are ignored).
