# Recipe Quality Improvement Process

How to systematically improve a ZCP recipe. Written from the Laravel recipe overhaul (Mar 2026) as a reusable template for all recipes.

---

## Overview

Each recipe in `internal/knowledge/recipes/` is a framework-specific guide that an LLM uses to bootstrap and configure applications on Zerops. Recipes must contain ONLY Zerops-specific knowledge — patterns, gotchas, and wiring that the LLM cannot derive from general training data.

This document describes the process for auditing and improving a recipe, designed to be executed by a fresh agent with no prior context.

---

## Phase 1: Audit Current Recipe

### 1.1 Read the recipe
Read `internal/knowledge/recipes/{name}.md` completely.

### 1.2 Verify against live platform
For EVERY claim in the recipe, verify against the real Zerops platform:

**Env var names**: Create the managed services referenced in the recipe via `zerops_import`, then `zerops_discover includeEnvs=true`. Compare discovered service-level env var names against what the recipe uses in `${hostname_varName}` refs.

Known issues found in Laravel audit:
- Valkey exposes `hostname`, NOT `host` — recipe using `${redis_host}` would fail silently
- MariaDB sets `user` and `dbName` equal to the service hostname (not static values like PostgreSQL)
- Object Storage has no `connectionString` — uses individual `accessKeyId`, `secretAccessKey`, etc.

**APP_KEY / secrets**: Check if the recipe puts framework secrets (APP_KEY, SECRET_KEY_BASE, etc.) in import.yml `envSecrets`. This creates per-service secrets — in a dev+stage setup, each service gets a DIFFERENT random value. Secrets that must be shared across services belong at **project level** via `zerops_env project=true`.

Also verify the secret format. Laravel's `<@generateRandomString(<32>)>` generates plaintext, but Laravel requires `base64:` prefix + 32 base64-encoded bytes. Each framework has its own format requirements.

**Static vs dynamic env vars**: Check if any env vars are hardcoded where they should use `${hostname_varName}` refs:
- `DB_HOST: db` → should be `${db_hostname}`
- `DB_PORT: 5432` → should be `${db_port}`
- `REDIS_HOST: redis` → should be `${cache_hostname}`

Rule: ALL values that come from a managed service must use dynamic refs. Even if the value seems obvious (`5432` for PostgreSQL), it decouples the recipe from specific hostnames and makes the pattern consistent.

**initCommands**: Check for:
- `--isolated` flag on migrations — breaks with `CACHE_STORE=database` (chicken-and-egg: lock needs cache table, migration creates cache table). `zsc execOnce` already handles concurrency.
- Redundant commands — `php artisan optimize` already includes `config:cache`, `route:cache`, `view:cache`, `event:cache`. Don't list them separately.
- Commands that should only be in stage, not dev (e.g., `optimize` slows dev iteration).

### 1.3 Cross-reference with Zerops docs
Read `../zerops-docs/` for the runtime and service types used in the recipe. Check:
- Correct service type versions (e.g., `mariadb@10.6`, not `mariadb@10.11`)
- Port numbers and protocols
- Any platform-specific behavior not covered in the recipe

### 1.4 E2E deployment test
Actually deploy the framework on Zerops and verify it works:

1. Import services via `zerops_import`
2. Set project-level secrets via `zerops_env project=true`
3. SSH into the container, scaffold the project
4. Write zerops.yml with ALL dynamic refs
5. Deploy via `zerops_deploy` (from zcpx) or E2E test binary
6. Hit `/status` endpoint — verify all managed service connections work
7. Document any issues encountered

This step catches problems that static analysis misses (e.g., wrong APP_KEY format causing HTTP 500, wrong Elasticsearch client version).

---

## Phase 2: Recipe Structure

Every recipe should follow this structure:

### Header
```markdown
# {Framework} on Zerops
{One-line description — what runtime, what it does}

## Keywords
{framework-specific terms for BM25 search}

## TL;DR
{3-line summary: runtime type, key config, critical pattern}
```

### Framework Secrets (if applicable)
How to generate and where to set framework-specific secrets. Always project-level for dev+stage shared secrets. Include the exact generation command and format.

### Wiring Managed Services
Reference the universal `${hostname_varName}` pattern. Don't re-document the full service vars table (it's in the runtime guides). Instead, show framework-specific MAPPING examples:

```
Framework env var → Zerops cross-ref
DATABASE_URL     → ${db_connectionString}
REDIS_HOST       → ${cache_hostname}
```

### Stack Layers
Build incrementally from minimal to full:
- **Layer 0**: Framework alone (no managed services) — stateless
- **Layer 1**: + Database — what changes
- **Layer 2**: + Cache — what changes
- **Layer 3**: + Storage — what changes
- **Layer 4+**: Additional services — reference the pattern

Each layer shows ONLY what to ADD — import.yml block + zerops.yml env vars to add/change. Don't repeat the full config.

### Dev vs Stage
Table of what differs. Typically:
- Environment/debug flags
- initCommands (dev: minimal, stage: optimizations)
- healthCheck/readinessCheck (dev: omit, stage: enable)
- buildCommands (dev: minimal, stage: full build pipeline)

Emphasize: managed service refs are SAME for both (shared services).

### Scaffolding
Framework-specific project setup commands. Note:
- Which flags prevent .env/.config file creation
- RAM requirements for package installation
- What to delete after scaffold

### Configuration
ONLY Zerops-specific config. Things the LLM would NOT know from general training:
- Proxy trust configuration (specific to Zerops L7 balancer)
- Log channel for Zerops collector
- Runtime-specific document root

### Gotchas
ONLY things that are:
1. Zerops-platform-specific (not general framework knowledge)
2. Non-obvious (would cause a real failure)
3. Verified (tested on live platform)

Bad gotcha: "Make sure your database credentials are correct" (generic)
Good gotcha: "Valkey var is `hostname`, not `host` — `${cache_host}` fails silently" (Zerops-specific, non-obvious, verified)

---

## Phase 3: Validation

### 3.1 Recipe lint test
Run `go test ./internal/knowledge/ -run TestRecipeLint -v` to verify structural integrity.

### 3.2 Content checks
- No `--isolated` in migration commands
- No `envSecrets: APP_KEY` (or framework equivalent) in import.yml examples
- All `${hostname_varName}` refs use verified var names
- Build base version matches run base version
- `os: ubuntu` on both build and run (unless recipe explicitly documents Alpine)

### 3.3 E2E smoke test
Deploy and verify all managed service connections work. This is the final gate.

---

## Known Patterns Across Frameworks

These apply to most/all recipes — check each one:

| Pattern | What to check |
|---------|--------------|
| **Framework secrets** | Must be project-level, not service envSecrets. Verify format (base64, hex, etc.) |
| **DB wiring** | All `${db_*}` refs. No static hostnames, ports, or db names |
| **Cache wiring** | Valkey var is `hostname` NOT `host`. No auth needed (private network) |
| **S3 wiring** | `AWS_USE_PATH_STYLE_ENDPOINT: "true"` required (MinIO). Region `us-east-1` required but ignored |
| **Migration concurrency** | Use `zsc execOnce`, never framework-level isolation flags |
| **Dev vs Stage** | Recipes should show both entries or clearly explain the differences |
| **initCommands** | Dev: minimal (just migrate). Stage: migrate + optimize/cache commands |
| **Health checks** | Stage only, never dev |
| **Build base version** | Must match run base version |
| **Proxy trust** | Framework-specific config for Zerops L7 balancer (`TRUSTED_PROXIES`, `CSRF_TRUSTED_ORIGINS`, etc.) |

---

## Recipes That Need This Process Applied

Priority order (most used first):
1. ~~laravel.md~~ (DONE — Mar 2026)
2. filament.md — mirrors Laravel, apply same fixes
3. twill.md — mirrors Laravel, apply same fixes
4. django.md
5. symfony.md
6. rails.md
7. nextjs.md
8. nestjs.md
9. phoenix.md
10. All others

For Laravel-derived recipes (filament, twill): apply the same changes as laravel.md — they share the same runtime, same gotchas, same wiring patterns. The only differences are framework-specific packages and init commands.
