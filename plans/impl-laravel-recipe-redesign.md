# Plan: Laravel Recipe Redesign

**Date**: 2026-03-24
**Based on**: E2E verified tests (11/11 managed services connected, all dynamic refs confirmed)
**Affected files**: `internal/knowledge/recipes/laravel.md`, `filament.md`, `twill.md`

---

## Principles

1. **Only Zerops-specific content** — no generic Laravel knowledge the LLM already has
2. **Pattern-first** — teach `${hostname_varName}` + discovery, not hardcoded templates
3. **Extensible** — adding any managed service follows the same pattern
4. **Dev/stage aware** — clearly separate what differs
5. **E2E verified** — every claim tested on live platform

---

## Recipe Structure

### 1. Header + Keywords + TL;DR

**Change**: Reframe from "Laravel with PostgreSQL" to capability-based.

```markdown
# Laravel on Zerops

Laravel on PHP-Nginx. Add managed services as needed — PostgreSQL or MariaDB
for database, Valkey for cache/sessions, Object Storage for S3 files.
All wired via `${hostname_varName}` cross-service references.

## Keywords
laravel, php, php-nginx, postgresql, mariadb, valkey, redis, s3, object-storage,
zerops.yml, documentRoot, artisan, composer

## TL;DR
PHP-Nginx runtime, port 80 fixed, `documentRoot: public`. APP_KEY on project level.
Wire managed services with `${hostname_varName}` refs in zerops.yml. Scaffold with
`composer create-project --no-scripts`. No `.env` files — Zerops injects OS env vars.
```

### 2. APP_KEY Setup (NEW section — currently broken)

**Why**: Current recipe uses `envSecrets: APP_KEY: <@generateRandomString(<32>)>` which:
- Generates invalid format (plaintext, not base64:+32bytes)
- Sets per-service (dev+stage get different keys → broken encryption)

**New content**:
```markdown
## APP_KEY

Set at PROJECT level (shared by all services). Laravel requires `base64:` prefix
with 32 random bytes base64-encoded.

Generate: `php -r "echo 'base64:'.base64_encode(random_bytes(32)).\"\\n\";"`
Set: `zerops_env project=true variables=["APP_KEY=base64:..."]`

Do NOT put APP_KEY in import.yml envSecrets — that creates per-service keys.
After changing APP_KEY: `php artisan config:clear` on the container.
```

### 3. Wiring Managed Services (NEW section — replaces hardcoded env vars)

**Why**: Current recipe hardcodes `DB_HOST: db`, `REDIS_HOST: redis`. Doesn't teach
the pattern, breaks with different hostnames, unusable for services not in the template.

**New content**:
```markdown
## Wiring Managed Services

Pattern: `${hostname_varName}` — `hostname` is your service name from import.yml,
`varName` is the service-level env var. Resolved at container start.

After adding any service: `zerops_discover includeEnvs=true` to see available vars.
Map ONLY discovered vars in zerops.yml envVariables.

### Available vars by service type

| Type | hostname | port | user | password | dbName | connectionString | Extra |
|------|:---:|:---:|:---:|:---:|:---:|:---:|-------|
| PostgreSQL | yes | 5432 | yes | yes | yes | `postgresql://...` | portTls, superUser |
| MariaDB | yes | 3306 | yes* | yes | yes* | `mysql://...` | *user,dbName = hostname |
| Valkey/KeyDB | yes | 6379 | — | — | — | `redis://...` | portTls. No auth. |
| Object Storage | yes | — | — | — | — | — | accessKeyId, secretAccessKey, apiUrl, bucketName |
| Meilisearch | yes | 7700 | — | — | — | `http://...` | masterKey, defaultSearchKey, defaultAdminKey |
| Elasticsearch | yes | 9200 | yes | yes | — | `http://...` | |
| Typesense | yes | 8108 | — | — | — | `http://...` | apiKey |
| ClickHouse | yes | 9000 | yes | yes | yes | `clickhouse://...` | portHttp(8123), portMysql, portPostgresql |
| Qdrant | yes | 6333 | — | — | — | `http://...` | apiKey, readOnlyApiKey, grpcPort |
| NATS | yes | 4222 | yes | yes | — | `nats://...` | portManagement, JET_STREAM_ENABLED |
| Kafka | yes | 9092 | yes | yes | — | — | |

### Laravel mapping examples

Database (hostname `db`):
  DB_HOST: ${db_hostname}
  DB_PORT: ${db_port}
  DB_DATABASE: ${db_dbName}
  DB_USERNAME: ${db_user}
  DB_PASSWORD: ${db_password}

Valkey (hostname `cache`):
  REDIS_HOST: ${cache_hostname}     # NOTE: var is "hostname", NOT "host"
  REDIS_PORT: ${cache_port}
  REDIS_CLIENT: phpredis            # pre-installed in php-nginx, NOT predis

Object Storage (hostname `storage`):
  AWS_ACCESS_KEY_ID: ${storage_accessKeyId}
  AWS_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
  AWS_BUCKET: ${storage_bucketName}
  AWS_ENDPOINT: ${storage_apiUrl}
  AWS_URL: ${storage_apiUrl}/${storage_bucketName}
  AWS_USE_PATH_STYLE_ENDPOINT: "true"    # required (MinIO backend)
  AWS_DEFAULT_REGION: us-east-1          # required but ignored

Meilisearch (hostname `search`) — for Laravel Scout:
  SCOUT_DRIVER: meilisearch
  MEILISEARCH_HOST: http://${search_hostname}:${search_port}
  MEILISEARCH_KEY: ${search_masterKey}
```

### 4. zerops.yml — Dev and Stage entries (REWRITE)

**Why**: Current recipe has single entry with production-only config. No dev/stage guidance.

**New content** — two entries clearly labeled:

```markdown
## zerops.yml

### Dev entry
- `APP_ENV: local`, `APP_DEBUG: "true"`, `LOG_LEVEL: debug`
- `deployFiles: ./`
- initCommands: only `migrate --force` (no optimize — speeds iteration)
- No healthCheck, no readinessCheck (agent controls lifecycle)
- build: `composer install --ignore-platform-reqs` (no --no-dev, no npm)

### Stage entry (added after dev is verified)
- `APP_ENV: production`, `APP_DEBUG: "false"`, `LOG_LEVEL: info`
- `deployFiles: ./`
- initCommands: `migrate --force` + `optimize`
- healthCheck + readinessCheck on `/up`
- build: `--no-dev`, + nodejs@22 + npm build if assets
- Managed service refs: SAME as dev (shared services)
```

Show both entries as concrete YAML. envVariables section same for both — only
APP_ENV, APP_DEBUG, LOG_LEVEL, initCommands, and health checks differ.

### 5. import.yml — Standard mode (REWRITE)

**Why**: Current has single `app` service + wrong APP_KEY in envSecrets.

**New content**: Show standard dev+stage pattern:
```yaml
services:
  - hostname: appdev
    type: php-nginx@8.4
    startWithoutCode: true
    maxContainers: 1
    enableSubdomainAccess: true

  - hostname: appstage
    type: php-nginx@8.4
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16        # or mariadb@10.6
    mode: NON_HA
    priority: 10
```

NO APP_KEY in import. Set via `zerops_env project=true` after import.

Show variants: "Add Valkey", "Add Object Storage" as additive blocks.

### 6. Scaffolding (KEEP, minor updates)

Already correct. Changes:
- Add `rm -f .env` (not just `.env.example` — be safe)
- Clarify: "Run on dev container only. Stage gets code via cross-deploy."

### 7. Configuration (TRIM)

Keep only Zerops-specific:
- **Trusted proxies** — Laravel 11+ wiring in bootstrap/app.php (KEEP as-is)
- **LOG_CHANNEL: syslog** (KEEP — Zerops-specific)
- **documentRoot: public** (KEEP)
- **phpredis** is pre-installed — use `REDIS_CLIENT: phpredis`, not `predis`

Remove:
- APP_KEY line (moved to own section)
- DB credentials line (covered by wiring section)
- AWS line (covered by wiring section)

### 8. Gotchas (REWRITE — Zerops-specific only)

Keep:
- **No `.env` file** — `--no-scripts` prevents shadowing
- **No SQLite** — filesystem volatile on deploy
- **os: ubuntu** on both build and run
- **Config cache after env change** — `php artisan config:clear`

Add:
- **`--isolated` on migrate is DANGEROUS** with `CACHE_STORE=database` — chicken-and-egg.
  `zsc execOnce` already handles concurrency. Never use `--isolated`.
- **Elasticsearch PHP client version must match server** — ES 8.16 needs `elasticsearch/elasticsearch:^8.0`,
  NOT v9 (sends incompatible version header)
- **Valkey has `hostname`, NOT `host`** — `${cache_host}` fails silently,
  use `${cache_hostname}`
- **MariaDB user/dbName = hostname** — not static like PostgreSQL's `db`.
  Always use dynamic refs.
- **`league/flysystem-aws-s3-v3`** required in composer.json for S3
- **`AWS_USE_PATH_STYLE_ENDPOINT: "true"`** required (MinIO backend)
- **Never use `php artisan serve`** — php-nginx has built-in webserver on port 80

Remove:
- **502 after deploy** — platform concern, not recipe
- **Session not persisting** — generic, covered by wiring section
- **Migration fails** — generic, covered by wiring section

### 9. Common Failures (TRIM)

Keep only failures that are non-obvious and Zerops-specific:
- **HTTP 500 with APP_KEY** — wrong format or missing. Must be `base64:` + 44 chars.
- **S3 driver not found** — add `league/flysystem-aws-s3-v3`
- **HTTP 500 check logs first** — `storage/logs/laravel.log` + `zerops_logs`
- **Never `php artisan serve`** on php-nginx

---

## Changes to Other Recipes

### filament.md
Same changes as laravel.md:
- Remove `--isolated` from initCommands
- Dynamic env var refs
- Remove APP_KEY from envSecrets
- Dev/stage entries (if applicable)

### twill.md
Same as filament.

### echo-go.md, spring-boot.md, java-spring.md
- Make DB_HOST dynamic: `${db_hostname}`

### Workflow guidance (wiring patterns in bootstrap)
- Fix table: Valkey var is `hostname` not `host`
- Add: "APP_KEY for Laravel — set at project level via zerops_env"

---

## What we are NOT changing

- Scaffolding commands (already correct)
- Trusted proxies PHP code example (already correct)
- The `${hostname_varName}` resolution mechanism (platform feature, works correctly)
- Service cards in workflow guidance (separate from recipes)

---

## Implementation Order

| Step | What | Risk |
|------|------|------|
| 1 | laravel.md full rewrite | LOW — recipe is documentation, no code change |
| 2 | filament.md + twill.md mirror changes | LOW |
| 3 | Other recipes: dynamic DB_HOST | LOW |
| 4 | Workflow guidance: fix Valkey var name, add APP_KEY note | LOW |
| 5 | Recipe lint test: add `--isolated` detection | LOW |
| 6 | E2E test: permanent `laravel_recipe_test.go` | LOW |

All changes are knowledge/documentation. No Go code changes needed (except lint test + E2E test).
