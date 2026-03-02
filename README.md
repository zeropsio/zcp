# ZCP — Zerops Control Plane

Single Go binary. MCP server over STDIO. Runs inside a `zcp@1` service in the project it manages.

LLM connects via Claude Code, calls MCP tools, ZCP translates to Zerops API. No subprocess shelling, no intermediate CLI — direct API calls.

## Why

Previously two binaries: `zaia` (Go CLI with business logic) + `zaia-mcp` (MCP wrapper that shelled out to zaia). Subprocess overhead, double serialization, error propagation friction. ZCP merges both into one process.

## How it works

```
Claude Code ←→ STDIO (JSON-RPC) ←→ ZCP binary ←→ Zerops API
```

On startup, ZCP:
1. Resolves auth (env var `ZCP_API_KEY` or zcli token fallback)
2. Validates token, discovers project ID
3. Builds system prompt with project state (service list, routing directives)
4. Registers MCP tools, starts STDIO transport

The system prompt tells the LLM which workflow to start based on project state. The LLM drives from there — ZCP is reactive, not prescriptive.

## Architecture

```
cmd/zcp/main.go        Entrypoint, subcommands (init, version, update), MCP server

internal/
  server/              MCP server setup, tool registration, system prompt builder
  tools/               MCP tool handlers — thin wrappers that validate input, call ops
  ops/                 Business logic — validation, orchestration, caching
  platform/            Zerops API client, types, error codes
  auth/                Token resolution, project discovery
  knowledge/           BM25 search engine, embedded platform docs + recipes
  workflow/            Bootstrap conductor, session state, phase gates
  content/             Embedded workflow guides + templates (go:embed)
  runtime/             Zerops container vs local detection
  init/                `zcp init` — config file generation
  update/              Self-update from GitHub releases
```

## Key concepts

### Tools

MCP tools are what the LLM calls. Each tool is a thin handler in `internal/tools/` that validates input and delegates to `internal/ops/`. Two categories:

**Read-only**: discover, knowledge, logs, events, process, verify, workflow
**Mutating**: deploy, manage, scale, env, import, delete, subdomain, mount

Deploy and mount only work inside Zerops containers (need SSH/SSHFS to sibling services on the VXLAN network). When running locally, these tools are not registered.

### Workflows

The LLM doesn't freestyle — it follows workflow guides. `zerops_workflow` is the entry point for every operation. Workflow guides are markdown files embedded at compile time (`internal/content/workflows/`).

**bootstrap** is the main one. It's a stateful 11-step conductor that takes a project from empty to fully deployed:

detect → plan → load-knowledge → generate-import → import-services → mount-dev → discover-envs → generate-code → deploy → verify → report

Each step has: guidance text (what to do), required tools, verification criteria, and a skippable flag. The conductor enforces sequential progression — the LLM can't skip ahead.

Other workflows (deploy, debug, scale, configure, monitor) are stateless — just markdown guides returned to the LLM.

### Knowledge system

Platform knowledge is embedded in the binary via `go:embed`. Three layers:

- **Themes** (`knowledge/themes/`) — core platform rules, runtime specifics, managed service reference, architecture decisions
- **Recipes** (`knowledge/recipes/`) — framework-specific configs (Laravel, Next.js, Django, etc.)
- **Guides** (`knowledge/guides/`) — operational topics (scaling, networking, CI/CD, etc.)

`zerops_knowledge` has four modes: text search (BM25), contextual briefing (layered composition for a specific stack), recipe retrieval, infrastructure scope (full YAML schema reference).

### Deploy model

ZCP runs inside the project as a `zcp@1` service. It deploys to sibling services via SSH over the private VXLAN network. The flow:

1. Mount target service filesystem via SSHFS
2. Write code + zerops.yml to mount path
3. `zerops_deploy` triggers SSH push (git init + zcli push on the target)
4. Zerops build pipeline runs, container restarts with deployed code

Dev services use `deployFiles: [.]` (source deploy). Stage services use build output.

### System prompt

Built dynamically at startup by `instructions.go`. Contains:
- Routing table (which workflow to start for which task)
- Active workflow hint (if resuming a session)
- Runtime context (service name when inside Zerops)
- Project summary (service list + project state classification)

Project state drives the routing: FRESH/empty → bootstrap, CONFORMANT → deploy, NON_CONFORMANT → bootstrap alongside existing services.

## Development

```bash
go test ./... -count=1 -short    # All tests, fast
go test ./... -count=1 -race     # All tests with race detection
go build -o bin/zcp ./cmd/zcp    # Build
make lint-fast                   # Lint (~3s)
```

E2E tests need a real Zerops project: `go test ./e2e/ -tags e2e` (requires `ZCP_API_KEY` or zcli login).
