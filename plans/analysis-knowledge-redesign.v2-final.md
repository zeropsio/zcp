# Knowledge System Redesign v2 — Final Proposal

**Date**: 2026-04-02
**Status**: Ready for implementation
**Based on**: 7 agent analyses, 700+ line delivery map, full codebase deep-dive

---

## Architecture Summary

4 layers, each with clear identity, delivery mechanism, and audience:

| Layer | Identity | Content | Delivery | Size |
|-------|----------|---------|----------|------|
| **1. Platform Model** | How Zerops works | Container Universe, Lifecycle, Networking, Storage, Scaling, Base Image | NEW: `model.md`, injected at discover step + in scope=infrastructure | ~60L |
| **2. Constraints** | What MUST/NEVER | Hard rules with one-line reasons | `universals.md`, prepended to recipes | ~35L |
| **3. Flow Knowledge** | YAML schemas + rules | import.yml/zerops.yml schemas, Rules & Pitfalls, Schema Rules, Examples | `core.md` H2 sections, extracted per-step by getCoreSection() | ~310L |
| **4. Dynamic** | Stack-specific + on-demand | Services, recipes, guides, decisions, bases | Briefing (7-layer), query, recipe mode | varies |

---

## Why This Structure

### Codebase facts that drive the design:

**1. System prompt has 2KB limit** (`instructions.go:37`). Platform Model CANNOT go there.

**2. getCoreSection() extracts exactly 4 H2 sections** from core.md:
- `"import.yml Schema"` — provision step
- `"zerops.yml Schema"` — generate step
- `"Rules & Pitfalls"` — generate step
- `"Schema Rules"` — deploy step

The other H2 sections (Container Universe, Networking, Storage, Scaling, Immutable Decisions, Base Image) are NEVER extracted individually — they exist only in `scope=infrastructure` full dump. They are effectively **dead weight** in core.md from a per-step delivery perspective.

**3. Workflows are self-contained** — bootstrap.md embeds ~20 platform rules directly. Agent doesn't NEED external knowledge for normal flow. Duplication with themes/ is intentional.

**4. GetUniversals() is called from 2 places**: scope mode prepend + recipe prepend. Every recipe response includes universals.md content. Must be concise.

**5. Discover step injects NO knowledge today** — agent gets only static guidance from bootstrap.md. Adding model injection is a ~8 LOC change.

---

## File Changes

### NEW: `themes/model.md` (~60L)

Content extracted from core.md Platform Model sections. Serves two purposes:
- Injected at discover step (conceptual foundation before agent plans stack)
- Included in scope=infrastructure response (reference completeness)

```markdown
# Zerops Platform Model

How Zerops works — the mental model for understanding all Zerops configuration.

## Container Universe

Everything on Zerops runs in full Linux containers (Incus, not Docker). Each container has:
- Full SSH access, working directory /var/www
- Connected via VXLAN private network (per project)
- Addressable by service hostname (internal DNS)
- Own disk (persistent, grow-only)

Hierarchy: Project > Service > Container(s). One project = one isolated network.

Two core plans: Lightweight (15h build, 5GB backup, 100GB egress) and
Serious ($10 one-time, 150h build, 25GB backup, 3TB egress, HA support).

## The Two YAML Files

| File | Purpose | Scope |
|------|---------|-------|
| import.yml | Topology — WHAT exists | Services, types, versions, scaling, env vars |
| zerops.yml | Lifecycle — HOW it runs | Build, deploy, run commands per service |

Separate concerns. import.yml creates infrastructure. zerops.yml defines what happens on push.

## Build/Deploy Lifecycle

Source Code → BUILD CONTAINER (prepareCommands → buildCommands → deployFiles)
  → deployFiles = THE ONLY BRIDGE →
RUN CONTAINER (prepareCommands → deploy files arrive → initCommands → start)

Phase ordering:
1. build.prepareCommands — install tools, cached in base layer
2. build.buildCommands — compile, bundle, test
3. build.deployFiles — select artifacts to transfer
4. run.prepareCommands — customize runtime image (runs BEFORE deploy files arrive!)
5. Deploy files arrive at /var/www
6. run.initCommands — per-container-start tasks (migrations)
7. run.start — launch the application

Critical: run.prepareCommands runs BEFORE deploy files are at /var/www.

## Networking

Internet → L7 Load Balancer (SSL termination) → container VXLAN IP:port → app

L7 LB terminates SSL/TLS — all internal traffic is plain HTTP.
Valid port range: 10-65435 (80/443 reserved; exception: PHP uses 80).

## Storage

- Container disk: per-container, persistent, grow-only
- Shared storage: NFS mount at /mnt/{hostname}, POSIX-only, max 60 GB
- Object storage: S3-compatible (MinIO), forcePathStyle required, one bucket per service

## Scaling

- Vertical: CPU (shared/dedicated), RAM, Disk (grow-only). Runtimes AND managed services.
- Horizontal: 1-10 containers for runtimes only. Managed: NON_HA=1, HA=3 (fixed, immutable).
- Docker: fixed resources only, resource change triggers VM restart.

## Base Image Contract

| Base | OS | Package Manager | libc |
|------|----|----------------|------|
| Alpine (default) | Alpine Linux | sudo apk add --no-cache | musl |
| Ubuntu | Ubuntu | sudo apt-get update && sudo apt-get install -y | glibc |

NEVER cross them: apt-get on Alpine = "command not found".
```

### EDIT: `themes/universals.md` (77→~35L)

Remove duplicates and non-constraints. Keep hard rules with one-line reasons:

```markdown
# Platform Constraints

Non-negotiable rules. Violating any causes failures.

## Networking
- MUST bind `0.0.0.0` (not localhost). L7 LB routes to container VXLAN IP. Binding localhost = 502.
- Internal traffic = plain HTTP. NEVER `https://` between services. SSL terminates at L7 balancer.
- MUST trust proxy headers (`X-Forwarded-For`, `X-Forwarded-Proto`). Framework config: see runtime recipe ## Gotchas.

## Containers & Filesystem
- Deploy = new container. Local files LOST. Only `deployFiles` content survives.
- Restart/reload/stop-start/vertical scaling = same container. Local files intact.
- Persistent data: database, object storage, or shared storage. NEVER local filesystem.
- `deployFiles` MANDATORY in zerops.yml build section. Without it, run container starts empty.

## Build Pipeline
- Build and Run = SEPARATE containers with separate base images and separate filesystems.
- `deployFiles` = the ONLY bridge. Build artifacts not listed don't exist at runtime.
- `run.prepareCommands` runs BEFORE deploy files arrive. Never reference `/var/www/` there.

## Environment Variables
- Zerops injects as OS env vars. Do NOT create `.env` files — empty values shadow OS vars.
- Cross-service wiring: `${hostname_varname}` in zerops.yml `run.envVariables`. Resolved at container start.
- import.yml service level: `envSecrets` ONLY (not `envVariables` — project-level only).
- Shared secrets (APP_KEY, SECRET_KEY_BASE): MUST be project-level, not per-service envSecrets.

## Multi-Container Safety
- Migrations: `zsc execOnce ${appVersionId} -- <cmd>` in `initCommands`. Prevents duplicate execution in HA.
- Sessions: external store (Valkey, database) when running >1 container.

## Immutable Decisions
- Hostname, mode (HA/NON_HA), object storage bucket, service type — CANNOT change after creation.
- Managed services: `priority: 10` (start before runtime services).
```

**What's removed** (was in old universals.md, no longer needed here):
- Container Lifecycle section → merged into "Containers & Filesystem" (was duplicate of Filesystem)
- Stack Composition section → moved to model.md or dropped (concept, not constraint)
- Deploy Mode Patterns section → lives in workflow docs (convention, not constraint)
- Per-framework proxy examples → already moved to recipe ## Gotchas

### EDIT: `themes/core.md` (~430→~310L)

Remove Platform Model sections (moved to model.md):
- Container Universe (~20L) → model.md
- The Two YAML Files (~8L) → model.md
- Build/Deploy Lifecycle (~35L) → model.md
- Networking (~10L) → model.md
- Storage (~5L) → model.md
- Scaling (~6L) → model.md
- Immutable Decisions (~7L) → model.md (also in universals as constraint)
- Base Image Contract (~10L) → model.md

**What stays in core.md** (~310L):
- STOP warning (don't generate from this reference alone)
- TL;DR + Keywords (updated: "YAML generation reference: schemas, rules, examples")
- import.yml Schema (~65L)
- zerops.yml Schema (~35L)
- Rules & Pitfalls (~50L — already cleaned of runtime-specific rules)
- Schema Rules (~80L — deploy semantics, tilde, cache, public access, zsc)
- Causal Chains (~12L — 7 universal rows)
- Multi-Service Examples (~90L — 4 complete import.yml examples)

core.md becomes purely "YAML generation reference" — the bag of H2 sections extracted by getCoreSection().

### services.md, operations.md, bases/, recipes/, guides/, decisions/

**No changes.** Already in correct state from v1 implementation.

---

## Code Changes

### 1. Document embedding (`documents.go:10`)

No change needed — model.md is in `themes/` which is already embedded.

### 2. GetPlatformModel or GetCore adjustment

**Option A (simple)**: `scope=infrastructure` assembles model + universals + core:
```go
// tools/knowledge.go scope handler:
model, _ := store.Get("zerops://themes/model")
if model != nil {
    result = model.Content + "\n\n---\n\n" + result
}
```

**Option B (cleaner)**: New Provider method `GetModel()`:
```go
func (s *Store) GetModel() (string, error) {
    doc, err := s.Get("zerops://themes/model")
    if err != nil {
        return "", fmt.Errorf("platform model not found: %w", err)
    }
    return doc.Content, nil
}
```

### 3. Discover step injection (`guidance.go`)

```go
// In assembleKnowledge(), add before existing provision check:
if params.Step == StepDiscover {
    if model, err := params.KP.GetModel(); err == nil && model != "" {
        parts = append(parts, model)
    }
}
```

~8 lines of code. Test: verify discover guidance includes "Container Universe."

### 4. scope=infrastructure update (`tools/knowledge.go:94-121`)

Prepend model content before universals + core:
```go
if hasScope {
    core, _ := store.GetCore()
    result := core
    if universals, _ := store.GetUniversals(); universals != "" {
        result = universals + "\n\n---\n\n" + result
    }
    // NEW: prepend platform model
    if model, _ := store.GetModel(); model != "" {
        result = model + "\n\n---\n\n" + result
    }
    // live stacks...
}
```

### 5. Tests

- `guidance_test.go`: Test discover step includes "Container Universe"
- `knowledge_test.go`: Test scope=infrastructure includes model content
- `store_access_test.go`: Add model.md to mock store

---

## Implementation Sequence

```
Phase 1: Content restructure (no code changes)
  1. Create themes/model.md from core.md Platform Model sections
  2. Remove Platform Model sections from core.md
  3. Rewrite universals.md (77→35L — constraints only)
  4. Verify: go test ./internal/knowledge/... passes

Phase 2: Code changes (TDD)
  5. RED: Test discover step includes model content
  6. GREEN: Add GetModel() + discover injection in guidance.go
  7. RED: Test scope=infrastructure includes model
  8. GREEN: Update tools/knowledge.go scope handler
  9. Update mock store in tests

Phase 3: Validation
  10. go test ./... -count=1 -short (all 17 packages)
  11. make lint-fast
  12. Manual: verify each knowledge mode output
```

---

## What Agent Sees After Redesign

### Recipe call: `zerops_knowledge recipe="nextjs-ssr"`
```
[35L constraints — networking, filesystem, env vars, build pipeline, multi-container, immutable]
---
[nextjs-ssr recipe content with ## Key Configuration Points]
```

### Scope call: `zerops_knowledge scope="infrastructure"`
```
[live service stacks]
---
[60L platform model — Container Universe, Lifecycle, Networking, Storage, Scaling, Base Image]
---
[35L constraints]
---
[310L YAML reference — schemas, rules, examples]
```

### Bootstrap discover step:
```
[static guidance from bootstrap.md discover section]
---
[60L platform model — agent understands how Zerops works before planning stack]
```

### Bootstrap provision step:
```
[static guidance from bootstrap.md provision section]
---
[import.yml schema from core.md]
```

### Bootstrap generate step:
```
[static guidance from bootstrap.md generate-{mode} section]
---
[runtime briefing (hello-world recipe with ## Gotchas)]
[dependency briefing (service cards + wiring)]
[discovered env vars]
[zerops.yml schema from core.md]
[Rules & Pitfalls from core.md]
```

---

## Decision Summary

| Decision | Rationale |
|----------|-----------|
| Create model.md (not inject in system prompt) | 2KB MCP limit prevents system prompt injection |
| Universals ~35L (not 15L, not 77L) | 15L too terse for recipe context; 77L has duplicates/concepts |
| Remove Platform Model from core.md | NEVER extracted by getCoreSection(); dead weight in per-step delivery |
| Keep Platform Model in scope=infrastructure | Reference completeness for ad-hoc agents |
| Inject model at discover step | Agent needs platform understanding BEFORE planning stack |
| core.md = "YAML reference" only | Matches actual usage (4 H2 sections extracted) |
| Stack Composition concept dropped from universals | Concept, not constraint. Can go in model.md or workflow docs later. |
| Deploy Mode Patterns dropped from universals | Convention, not constraint. Already in workflow docs. |
