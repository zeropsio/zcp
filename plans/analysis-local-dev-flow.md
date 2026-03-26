# Analysis: Local Development Flow for ZCP

**Date**: 2026-03-26
**Task**: Deep research on how to implement the local development flow where the dev environment runs on the user's local machine (with ZCP + zcli + VPN to the Zerops project). Code is local, no SSHFS mounts, dev server runs locally. Managed services remain on Zerops. Design the most elegant integration.
**Task type**: implementation-planning
**Complexity**: Deep (ultrathink, 4 agents)

## Reference files

### Current Architecture (container mode)
- `internal/workflow/environment.go` (19L) — Environment detection, EnvLocal constant defined but unused
- `internal/workflow/engine.go` (351L) — Main workflow engine, orchestrates steps
- `internal/workflow/deploy_guidance.go` (270L) — Deploy guidance with env-aware branching (EnvContainer checks)
- `internal/workflow/bootstrap_guidance.go` (143L) — Bootstrap guidance assembly
- `internal/workflow/bootstrap.go` (337L) — Bootstrap state types and step definitions
- `internal/workflow/bootstrap_steps.go` (44L) — 5-step definitions (discover, provision, generate, deploy, close)
- `internal/workflow/service_meta.go` (118L) — Persistent per-service metadata
- `internal/workflow/state.go` (38L) — WorkflowState types

### Deploy Flow
- `internal/ops/deploy.go` (212L) — SSH-only deploy via zcli push inside container
- `internal/ops/mount.go` (342L) — SSHFS mount operations (container-specific)
- `internal/tools/deploy.go` (149L) — MCP deploy tool registration with runtime-aware description
- `internal/ops/verify.go` (314L) — Health verification via platform API + HTTP checks
- `internal/ops/verify_checks.go` (253L) — Verification rule implementations

### Content & Guidance
- `internal/content/workflows/bootstrap.md` (864L) — Bootstrap step guidance (container-centric)
- `internal/content/workflows/deploy.md` (219L) — Deploy workflow guidance with strategy sections
- `docs/spec-bootstrap-deploy.md` (500+L) — Authoritative spec (explicitly scoped to container mode only)

### Knowledge
- `internal/knowledge/` — BM25 search engine, recipes, runtime guides
- `internal/runtime/runtime.go` (30L) — Container vs local detection

## Key Observations from Current Architecture

1. **EnvLocal exists but is completely unused** — `environment.go` defines it, `DetectEnvironment()` returns it, but no code path branches on `EnvLocal`
2. **Deploy is SSH-only** — `ops/deploy.go` SSHes into a container, runs `git init + zcli push` inside the container
3. **Mount is SSHFS-only** — designed for container-to-container filesystem access
4. **Verify uses platform API** — HTTP health checks go through Zerops subdomain URLs
5. **Guidance has one env branch** — `deploy_guidance.go:56-63` shows the only `EnvContainer` check (SSHFS mount path info)
6. **Spec explicitly defers local mode** — "Local mode shares concepts but has its own specifics (not covered here)"
7. **Bootstrap assumes container paths** — all file writes go to `/var/www/{hostname}/`
8. **zcli push from local is the standard Zerops deploy mechanism** — users normally run `zcli push` from their project directory

## Core Question

How should the bootstrap, deploy, and iteration workflows change when:
- Code lives in a local directory on the user's machine (not in a container)
- Dev server runs locally (user starts it or ZCP starts it)
- Managed services (DB, cache, etc.) are on Zerops, reachable via VPN
- No SSHFS mounts — files are native local filesystem
- `zcli push` runs directly from the local machine (not via SSH into a container)
- User has VPN connectivity to the Zerops project for service access
