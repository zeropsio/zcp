# Analysis: Laravel Recipe Eval Session — voicex Headless Logs

**Date**: 2026-03-24
**Task type**: codebase-analysis
**Complexity**: Deep (ultrathink)
**Session**: `83c947ba-61f4-45c0-bfa0-916890145fc1` on voicex (voice-driven, SDK CLI entrypoint)
**Trigger**: User voice command: "Okay, now create Laravel, the demonstration of ability to upload files to S3."

## Summary

Analysis of Claude headless session logs from voicex reveals 5 distinct issues with the Laravel recipe and bootstrap workflow. The most critical is a **chicken-and-egg bug** in the recipe's `initCommands`: the `--isolated` migration flag requires a cache store, but with `CACHE_STORE=database` the cache table doesn't exist until migrations run. Secondary issues include recipe-to-dev-mode mismatch (production env on dev service), and a historical design flaw where migration isolation is redundant when `zsc execOnce` is already used.

## Session Timeline

| Time (UTC) | Event |
|------------|-------|
| 10:31:06 | Voice prompt received |
| 10:31:10 | LLM receives knowledge (universals + PHP + Laravel recipe) |
| 10:31:35 | Discover step complete — plan: `laraveldev` (php-nginx@8.4, dev mode) + db + storage |
| 10:31:46 | Import YAML submitted — 3 services created |
| 10:32:49 | Env var discovery — all services ACTIVE |
| 10:33:19 | zerops.yml + app code generated |
| 10:33:27 | SSH deploy triggered |
| 10:34:42 | **DEPLOY_FAILED** — initCommands error |
| 10:35:57 | Error: `php artisan migrate --isolated --force` exit code 1 (QueryException) |
| 10:36:05 | LLM reads events, sees DEPLOY_FAILED |
| 10:36:23 | LLM reads runtime logs, sees the error |
| 10:36:43 | LLM removes `--isolated` from zerops.yml |
| 10:36:46 | Redeploy triggered |
| 10:38:03 | **DEPLOYED** — success |

## Findings by Severity

### Critical

| # | Finding | Evidence | Root Cause |
|---|---------|----------|------------|
| C1 | **`--isolated` migration flag causes DEPLOY_FAILED** when `CACHE_STORE=database` | Runtime logs: `QueryException` → `exit status 1` → `RUN.INIT COMMANDS FINISHED WITH ERROR`. The `--isolated` flag uses cache lock to prevent concurrent migration. With `CACHE_STORE=database`, it queries the `cache` table — which doesn't exist until migrations run. Chicken-and-egg. | Recipe `laravel.md:74` has `--isolated` in initCommands. `zsc execOnce` already prevents concurrent execution — `--isolated` is redundant AND dangerous. |

### Major

| # | Finding | Evidence | Root Cause |
|---|---------|----------|------------|
| M1 | **Recipe hardcodes `APP_ENV: production`** — wrong for dev mode services | Generated zerops.yml line 19: `APP_ENV: production` on `laraveldev` service. LLM copied from recipe verbatim. | Recipe `laravel.md:40` has `APP_ENV: production` without dev/prod variants. The recipe is designed as a production template, but the bootstrap workflow creates dev-mode services that copy it. |
| M2 | **Recipe doesn't account for dev-mode lifecycle** | Recipe has `initCommands` with `config:cache`, `route:cache`, `view:cache`, `optimize` — all production optimizations that break hot-reloading in dev. Recipe has `readinessCheck` which blocks dev containers. | Recipe is production-only. No guidance on which parts to strip for dev mode. The workflow guidance says "omit healthCheck for dev" but recipe doesn't indicate which initCommands are dev-safe. |
| M3 | **DB_HOST is static `db` in recipe — must be dynamic** | Recipe `laravel.md:46`: `DB_HOST: db` — hardcoded hostname. If DB service is named differently (e.g., `pgsql`, `database`), silently fails to connect. Also `DB_DATABASE: db` on line 45 is static. | Recipe couples to a specific hostname. Discovered env vars provide `db_hostname` (resolves to internal address) which works for DB connections. All DB vars should use dynamic refs: `${db_hostname}`, `${db_port}`, `${db_dbName}`, `${db_user}`, `${db_password}`. |

### Minor

| # | Finding | Evidence | Root Cause |
|---|---------|----------|------------|
| m1 | **LLM correctly used dynamic refs where recipe had static** | Generated `DB_DATABASE: ${db_dbName}` vs recipe's `DB_DATABASE: db`. LLM was more correct than the recipe. | The env var discovery protocol + workflow guidance ("ONLY use variables that were actually discovered") worked — the LLM used discovered vars. |
| m2 | **No Valkey service created** | Import had no redis/valkey. LLM correctly set `SESSION_DRIVER: database`, `CACHE_STORE: database`. But this created the `--isolated` conflict. | LLM correctly omitted Valkey (user asked for S3 demo, not cache), but the initCommands still had `--isolated` which needs a working cache store. |
| m3 | **`sudo -E -u zerops` was present** — user suspected missing sudo, but actual cause was different | zerops.yml line 47: `sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --isolated --force`. sudo was there. | User's hypothesis about missing sudo was incorrect. The actual error was QueryException during migration, not permission denied. |

## Root Cause Analysis

### The Isolated Migration Bug (C1) — Full Chain

1. Recipe `laravel.md:74` specifies: `sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --isolated --force`
2. `--isolated` in Laravel uses `Cache::lock()` to prevent concurrent migration runs
3. `CACHE_STORE: database` means the lock uses the `cache` database table
4. The `cache` table is created by `2024_xx_xx_create_cache_table` migration
5. But the migration command IS the thing creating that table
6. **Result**: `--isolated` tries to query a table that doesn't exist → QueryException → exit 1 → DEPLOY_FAILED

**Why `--isolated` is unnecessary**:
- `zsc execOnce ${appVersionId}` already guarantees single execution per deploy version
- Laravel's migration system tracks applied migrations in the `migrations` table — re-running `migrate` is inherently safe (skips already-applied)
- The user's insight is correct: "tohle se resi na urovni db" — this is handled at the DB level

### The Recipe-Dev Mismatch (M1 + M2)

The bootstrap workflow creates `laraveldev` (dev mode) but the recipe only has production config:
- `APP_ENV: production` → should be `local` for dev
- `APP_DEBUG: "false"` → LLM changed to `"true"`, correctly
- `initCommands` has production optimizations (config:cache, route:cache) → breaks dev hot-reloading
- `readinessCheck` and `healthCheck` → blocks dev containers

The workflow guidance says "omit healthCheck for dev" but the recipe doesn't have dev-variant markers.

## Recommendations

| # | Recommendation | Priority | Evidence | Effort |
|---|---------------|----------|----------|--------|
| R1 | **Remove `--isolated` from Laravel recipe** | P0 | Causes DEPLOY_FAILED with database cache store. `zsc execOnce` already handles concurrency. | 1 line change in `laravel.md:74` |
| R2 | **Remove `--isolated` from ALL recipes that use it** | P0 | Same bug applies to any recipe using `--isolated` with `CACHE_STORE=database`. Check all recipes. | Grep + batch fix |
| R3 | **Remove `--isolated` from universals.md** if referenced | P1 | Universals mentions `zsc execOnce` but may also mention `--isolated`. Remove to prevent recipe authors from adding it. | Check `universals.md` |
| R4 | **Add dev-mode markers to recipe** | P2 | Recipe should indicate which env vars and initCommands are prod-only. e.g., `# DEV: APP_ENV=local, APP_DEBUG=true` | ~10 lines in recipe |
| R5 | **Make ALL DB vars dynamic in recipe** | P1 | Change `DB_HOST: db` → `${db_hostname}`, `DB_DATABASE: db` → `${db_dbName}`. All 5 DB vars should use `${db_*}` refs. Static hostname coupling breaks when DB is named differently. | 2 line changes in laravel.md (+ filament, twill) |
| R6 | **Add recipe variant awareness to bootstrap workflow** | P3 | Workflow should strip prod-only initCommands when bootstrapping in dev mode | Workflow guidance update |
| R7 | **Validate recipe initCommands against envVariables** | P3 | Static analysis: if `--isolated` is present and `CACHE_STORE=database`, flag the conflict | Recipe lint test |

## Evidence Map

| Finding | Confidence | Basis |
|---------|-----------|-------|
| C1 (--isolated bug) | VERIFIED | Runtime logs from voicex session: `QueryException` → `exit status 1`. Second deploy without `--isolated` succeeded. |
| M1 (APP_ENV) | VERIFIED | Generated zerops.yml: `APP_ENV: production` on `laraveldev`. Recipe line 40: `APP_ENV: production`. |
| M2 (dev lifecycle) | VERIFIED | Recipe lines 71-75: production initCommands. Workflow guidance: "omit healthCheck for dev". |
| M3 (DB_HOST) | VERIFIED | Recipe line 46: `DB_HOST: db` (static). Discover response shows `db_hostname: evdt433f95` available as dynamic ref. Static coupling breaks if DB hostname changes. |
| m1 (LLM dynamic refs) | VERIFIED | Generated `DB_DATABASE: ${db_dbName}` vs recipe `DB_DATABASE: db`. |
| m2 (no Valkey) | VERIFIED | Import YAML: no redis service. zerops.yml: `CACHE_STORE: database`. |
| m3 (sudo present) | VERIFIED | zerops.yml line 47: sudo was present. Error was QueryException, not permission denied. |

## Self-Challenge Results

- **C1 confirmed**: Two deploys prove causation — first with `--isolated` failed, second without succeeded.
- **M1 confirmed**: Recipe has no dev variant. LLM copied verbatim.
- **M2 confirmed**: All initCommands are production optimizations.
- **M3 upgraded to MAJOR**: Static `DB_HOST: db` breaks when DB has different hostname. Must use `${db_hostname}`.
- **User's sudo hypothesis**: Disproven by evidence. sudo was present; error was QueryException.
