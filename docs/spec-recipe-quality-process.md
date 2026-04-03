# Recipe Quality Process

How to audit and improve a ZCP recipe. Designed for a fresh agent with no prior context.

---

## What a Recipe Is

A recipe is a framework-specific guide for deploying on Zerops. It's consumed by an LLM during bootstrap/deploy workflows. The LLM already knows the framework — it doesn't need the recipe to explain what Laravel or Django is.

**A recipe should contain ONLY things the LLM cannot derive from general training data.** That means Zerops-platform-specific patterns, gotchas, and configuration. If an LLM with no Zerops knowledge would get it wrong, it belongs in the recipe. If it would get it right, it doesn't.

---

## Audit Process

### 1. Verify every claim against live platform

Don't trust the recipe. Test it.

**Env var verification**: Create the managed services from the recipe via `zerops_import`, then run `zerops_discover includeEnvs=true`. Compare the ACTUAL service-level env var names against what the recipe uses in `${hostname_varName}` refs. Silent failures happen when a ref resolves to a literal string instead of a value — the platform gives no error.

**Secret format verification**: If the recipe sets framework secrets (via `envSecrets` or `zerops_env`), verify the generated value is in the format the framework actually expects. Deploy and check — format errors often manifest as HTTP 500 at runtime, not at import time.

**Secret scope verification**: If the recipe uses import.yml `envSecrets` on individual services, consider whether dev and stage services need the SAME secret (e.g., encryption keys, signing keys). If yes, the secret must be project-level (via `zerops_env project=true`), not per-service `envSecrets`.

**Deploy and test**: Actually scaffold the framework, deploy it, and hit the endpoints. This catches issues that static review misses — wrong client library versions, missing PHP extensions, incompatible config formats. See "E2E Testing" section below.

### 2. Check what's generic vs Zerops-specific

Read every line in the recipe and ask: "Would an LLM get this wrong without this line?"

**Remove if**: The LLM already knows it (framework docs, common patterns, standard config).

**Keep if**: It's a Zerops platform behavior, a non-obvious gotcha verified on live platform, or a specific env var / config pattern that only works on Zerops.

**Be skeptical of**: "Best practices" that are just general framework advice. "Configuration" sections that restate framework documentation.

### 3. Check the wiring pattern

Every recipe that uses managed services should wire them via `${hostname_varName}` — the universal Zerops cross-service reference pattern. Check:

- Are all managed service values wired dynamically? No hardcoded hostnames, ports, or database names.
- Does the recipe explain that `hostname` in `${hostname_varName}` comes from the import.yml service hostname? An agent who names their DB `mydb` instead of `db` needs to use `${mydb_hostname}`, not `${db_hostname}`.
- Does the recipe rely on vars that don't actually exist on the service? Verify each ref against `zerops_discover` output.

### 4. Check migration / init commands

- Does the recipe use `zsc execOnce ${appVersionId}` for migrations? This ensures single execution per deploy in multi-container setups.
- Does the recipe use any framework-level concurrency flags (e.g., Laravel's `--isolated`, Django's migration locks)? These may conflict with `zsc execOnce` or require specific infrastructure (cache store, lock backend) that might not exist yet at init time.
- Are init commands appropriate for dev vs stage? Heavy caching/optimization commands slow dev iteration.

### 5. Check dev vs stage awareness

- Does the recipe show only production config? If yes, an LLM creating a dev service will copy production settings (wrong debug flags, unnecessary health checks, optimization commands that break hot-reload).
- Are the differences between dev and stage config clear?
- Are managed services documented as shared (same DB for dev and stage)?

---

## E2E Testing

The most important part. Every recipe claim should be verified by deploying real code to real Zerops infrastructure.

### Test infrastructure

- E2E tests live in `e2e/` directory with `//go:build e2e` tag
- Tests use `e2eHarness` (creates MCP server + real API client) and `e2eSession` (MCP tool calls)
- Services are created via `zerops_import`, deployed via `zerops_deploy` (runs on zcpx via SSH)
- Cleanup uses hostname prefixes registered in `helpers_test.go` `testServicePrefixes`
- Run: `go test ./e2e/ -tags e2e -run TestName -count=1 -v -timeout 600s`
- Cross-compile for zcpx: `GOOS=linux GOARCH=amd64 go test -tags e2e -c ./e2e/ -o /tmp/e2e-test-linux`

### What to test

1. **Service env var discovery**: Import managed services, `zerops_discover includeEnvs=true`, verify expected env var names exist on each service. This catches wrong var names in recipes.

2. **End-to-end deploy**: Scaffold the framework on a container (SSH), write zerops.yml with all dynamic refs, deploy via `zerops_deploy`, verify `/status` endpoint returns OK for all managed service connections.

3. **Specific gotchas**: If the recipe documents a gotcha, write a test that would FAIL without the fix. E.g., wrong APP_KEY format → HTTP 500.

### Reference: `e2e/laravel_recipe_test.go`

The Laravel recipe test demonstrates the pattern:
- Creates all managed service types in one import
- Verifies service-level env var names via discover
- Tests project-level env var inheritance (APP_KEY use case)
- Logs all discovered vars for documentation

### Reference: `e2e/laravel_deploy_test.go`

The Laravel deploy test demonstrates full deploy verification:
- Uses `zerops_deploy` from zcpx (SSH-based, needs zcli in PATH)
- Enables subdomain after deploy
- Runs `zerops_verify` for health check
- Checks `zerops_logs` for init command errors

---

## Recipe Structure Guidelines

Not a rigid template — adapt to the framework. Some frameworks need layers (Laravel can work without a DB), others have a fixed minimum stack (Twill always needs DB + Redis + S3). Some are standalone recipes, others are diffs on top of a base (Filament = Laravel + 3 extras).

### What every recipe needs (lint-enforced)

- `## zerops.yml` — at least one valid YAML example with `zerops:` entries
- `## import.yml` — at least one valid import example (lint checks for content)
- `## Gotchas` — Zerops-specific gotchas (lint checks section exists)

### Content principles

**Only include what the LLM would get WRONG.** The LLM knows how to configure Django settings, write Express middleware, or set up Spring Boot properties. It does NOT know:
- Which Zerops env var names exist on which service types
- That `${hostname_varName}` is the wiring mechanism
- That Zerops L7 balancer requires specific proxy trust config
- That container filesystem is volatile across deploys
- Which PHP extensions are pre-installed vs need `apk add`
- That `zsc execOnce` is the migration concurrency mechanism

**Framework-specific packages on a "base" recipe**: When a framework (Filament, Twill) is built on another (Laravel), the recipe should reference the base recipe and document ONLY the differences. Don't duplicate the base. The LLM knows the relationship.

**Search finds recipes by title and content.** Use a descriptive title (e.g., "Filament on Zerops") so text search can disambiguate. The LLM can also find recipes by name via `zerops_knowledge recipe="filament"`.

---

## Validation

### Automated (lint)

```bash
go test ./internal/knowledge/ -run TestRecipeLint -v
```

Checks: title, keywords (>= 3), TL;DR, zerops.yml validity, import.yml presence, gotchas section, no stale URIs, no preprocessor in zerops.yml, versions against platform catalog.

### Manual review

- Every `${hostname_varName}` ref verified against `zerops_discover` output
- Secret format verified by actual deploy (not just reading docs)
- Dev vs stage differences documented where applicable
- No framework knowledge the LLM already has
- No managed service names in keywords

### E2E

Deploy the framework, hit endpoints, verify managed service connections. See "E2E Testing" section.

---

## Common Pitfalls When Writing Recipes

Things that went wrong in past audits — not rules to blindly apply, but patterns to watch for:

- **Hardcoded hostnames/ports**: `DB_HOST: db` works only if the service is named `db`. Dynamic refs decouple the recipe from specific naming.
- **Per-service secrets that should be shared**: Import.yml `envSecrets` generates a DIFFERENT value for each service. Encryption/signing keys shared across dev+stage must be project-level.
- **Framework-level migration locks + `zsc execOnce`**: Double-locking. The framework lock may require infrastructure (cache table, Redis) that doesn't exist at init time. `zsc execOnce` is sufficient.
- **Production-only config in recipes**: An LLM creating a dev service copies the recipe. If the recipe only shows production config, the dev service gets wrong debug/optimization settings.
- **Generic framework advice**: "Configure your database credentials correctly" — the LLM knows this. "Valkey env var is `hostname` not `host`" — the LLM doesn't know this.
- **Managed service names in keywords**: "postgresql" in a Laravel recipe keyword means searching "postgresql" returns Laravel instead of PostgreSQL docs.

---

## Status

| Recipe | Audited | E2E Tested | Date |
|--------|:---:|:---:|------|
| laravel | yes | yes (11 service types) | 2026-03-24 |
| filament | yes (diff on Laravel) | — | 2026-03-24 |
| twill | yes (diff on Laravel) | — | 2026-03-24 |
| symfony | yes | — | 2026-03-24 |
| django | — | — | — |
| rails | — | — | — |
| nextjs | — | — | — |
| All others | — | — | — |
