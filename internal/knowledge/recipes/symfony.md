# Symfony on Zerops

PHP-Nginx runtime with managed services. Build your stack layer by layer — start with just the app, add database, cache, mailer as needed.

## Keywords
symfony, doctrine, twig, encore, asset-map, webpack, monolog

## TL;DR
PHP-Nginx, `documentRoot: public`, port 80 fixed. APP_SECRET on project level (hex, 32 bytes). Wire services with `${hostname_varName}` refs. Scaffold: `symfony new` or `composer create-project`. No `.env` files.

## APP_SECRET

Must be **project-level** (shared across dev+stage). Symfony uses APP_SECRET for CSRF tokens, signed URLs, and remember-me cookies — dev and stage must share the same value.

**Generate**: `php -r "echo bin2hex(random_bytes(16)).\"\\n\";"`
**Set**: `zerops_env project=true variables=["APP_SECRET=<generated-hex>"]`

Do NOT use `envSecrets: APP_SECRET` in import.yml — generates a per-service secret (dev and stage get different values, breaking CSRF and signed URLs). Do NOT use `<@generateRandomString(<32>)>` — produces a random string without the correct hex format Symfony expects.

## Wiring Managed Services

Cross-service pattern: `${hostname_varName}` — resolved at container start from the target service's env vars.

After adding any service: `zerops_discover includeEnvs=true` to see available vars. Map ONLY discovered vars — guessing names causes silent failures (unresolved refs stay as literal strings).

**Critical**: the `X` in `${X_varName}` must match your actual service hostname from import.yml exactly. If your database hostname is `db`, use `${db_connectionString}`. If it is `postgres`, use `${postgres_connectionString}`.

### Service vars reference

| Type | hostname | port | user | password | dbName | connectionString | Auth extras |
|------|:---:|:---:|:---:|:---:|:---:|:---:|-------------|
| PostgreSQL | yes | 5432 | yes | yes | yes | `postgresql://...` | superUser, superUserPassword |
| MariaDB | yes | 3306 | yes* | yes | yes* | `mysql://...` | *user and dbName = service hostname |
| Valkey/KeyDB | yes | 6379 | — | — | — | `redis://...` | No auth (private network) |
| Object Storage | yes | — | — | — | — | — | accessKeyId, secretAccessKey, apiUrl, bucketName |
| Meilisearch | yes | 7700 | — | — | — | `http://...` | masterKey, defaultSearchKey, defaultAdminKey |
| Elasticsearch | yes | 9200 | yes | yes | — | `http://...` | — |

All types expose `hostname`, `port`, `connectionString` (except Object Storage). The pattern `${X_hostname}`, `${X_port}` works universally — `X` is your service hostname from import.yml.

### DATABASE_URL construction

Symfony expects a Doctrine-format DATABASE_URL. PostgreSQL's `connectionString` is `postgresql://user:password@host:port/dbName` — it already includes the database name. Append query params directly:

```yaml
DATABASE_URL: ${db_connectionString}?serverVersion=16&charset=utf8
```

Do NOT append `/${db_dbName}` — the connection string already contains the database name. Using `${db_connectionString}/${db_dbName}` results in a double-path URL that Doctrine cannot parse.

For MariaDB:
```yaml
DATABASE_URL: ${db_connectionString}?serverVersion=mariadb-10.6.0&charset=utf8
```

## Stack Layers

Build your stack incrementally. Each layer shows what to **add** to import.yml and zerops.yml.

### Layer 0: Just Symfony (file sessions)

Stateless app — API endpoints, docs sites. No persistent data.

**import.yml:**
```yaml
services:
  - hostname: appdev
    type: php-nginx@8.4
    startWithoutCode: true
    maxContainers: 1
    enableSubdomainAccess: true
```

**zerops.yml:**
```yaml
zerops:
  - setup: appdev
    build:
      base: php-nginx@8.4
      os: ubuntu
      buildCommands:
        - composer install --ignore-platform-reqs --no-dev --optimize-autoloader
        - php bin/console asset-map:compile
      deployFiles:
        - vendor
        - public/assets
        - .
      envVariables:
        APP_ENV: prod
    run:
      base: php-nginx@8.4
      os: ubuntu
      documentRoot: public
      envVariables:
        APP_ENV: dev
        APP_DEBUG: "true"
        LOG_CHANNEL: syslog
        TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"
```

Volatile filesystem — sessions lost on every deploy. OK for stateless APIs. `asset-map:compile` requires `APP_ENV: prod` at build time.

### Layer 1: + Database

Persistent data. PostgreSQL (recommended) or MariaDB.

**Add to import.yml:**
```yaml
  - hostname: db
    type: postgresql@16       # or mariadb@10.6
    mode: NON_HA
    priority: 10
```

**Add/change in zerops.yml envVariables:**
```yaml
        DATABASE_URL: ${db_connectionString}?serverVersion=16&charset=utf8
```

**Add initCommands:**
```yaml
      initCommands:
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php bin/console doctrine:migrations:migrate --no-interaction
```

`zsc execOnce` ensures only one container runs migrations in multi-container (HA) deployments. No lock bundle needed — `execOnce` provides the concurrency guard.

### Layer 2: + Cache/Sessions (Valkey)

In-memory sessions and cache — faster than filesystem, survives container restarts (but not deploys).

**Add to import.yml:**
```yaml
  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10
```

**Add/change in zerops.yml envVariables:**
```yaml
        REDIS_URL: ${cache_connectionString}
```

**Configure sessions in framework.yaml:**
```yaml
# config/packages/framework.yaml
framework:
    session:
        handler_id: '%env(REDIS_URL)%'
```

No password needed — Valkey runs on private network without auth. The `connectionString` for Valkey is `redis://hostname:6379`.

## Dev vs Stage zerops.yml

Managed services are **shared** — both dev and stage use the same `db` and `cache`. Only Symfony-specific config differs:

| | Dev | Stage |
|---|-----|-------|
| `APP_ENV` | `dev` | `prod` |
| `APP_DEBUG` | `"true"` | `"false"` |
| `LOG_LEVEL` | `debug` | `info` |
| `initCommands` | migrate only | migrate + `cache:warmup` |
| `healthCheck` | omit | add |
| `readinessCheck` | omit | add |
| Service refs | `${db_connectionString}`, `${cache_connectionString}`, ... | **same** |

**Both dev and stage** build with `APP_ENV: prod` — Symfony needs it for asset compilation and optimized autoloader. The run-time `APP_ENV` differs.

Stage zerops.yml additions (on top of dev):
```yaml
zerops:
  - setup: appstage
    deploy:
      readinessCheck:
        httpGet:
          port: 80
          path: /
    build:
      base: php-nginx@8.4
      os: ubuntu
      buildCommands:
        - composer install --no-dev --optimize-autoloader
        - php bin/console asset-map:compile
      deployFiles:
        - vendor
        - public/assets
        - .
      envVariables:
        APP_ENV: prod
    run:
      base: php-nginx@8.4
      os: ubuntu
      documentRoot: public
      envVariables:
        APP_ENV: prod
        APP_DEBUG: "false"
        LOG_LEVEL: info
        DATABASE_URL: ${db_connectionString}?serverVersion=16&charset=utf8
        REDIS_URL: ${cache_connectionString}
        TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"
      initCommands:
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php bin/console doctrine:migrations:migrate --no-interaction
        - php bin/console cache:warmup
      healthCheck:
        httpGet:
          port: 80
          path: /
```

`cache:warmup` on stage only — dev container uses the live kernel which warms on first request.

## Scaffolding

Fresh project setup on a Zerops dev container:

```bash
# Symfony CLI
zsc scale ram 1GiB 10m
symfony new . --no-git
rm -f .env .env.local .env.prod
php bin/console doctrine:migrations:migrate --no-interaction   # only if DB exists
zsc scale ram auto

# Alternative: Composer
zsc scale ram 1GiB 10m
composer create-project symfony/skeleton .
rm -f .env .env.local
zsc scale ram auto
```

- `zsc scale ram 1GiB 10m` — temporary RAM bump for composer (default 128 MB causes OOM)
- `rm -f .env*` — Symfony auto-loads `.env` files which shadow OS env vars. Remove them entirely
- Symfony's `.env` loader runs before the kernel; even an empty key `APP_SECRET=` overrides the real OS env var

## Configuration

- **TRUSTED_PROXIES** — required for Symfony behind Zerops reverse proxy. Set `TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"` and configure in `config/packages/framework.yaml`:
  ```yaml
  framework:
      trusted_proxies: '%env(TRUSTED_PROXIES)%'
      trusted_headers: ['x-forwarded-for', 'x-forwarded-host', 'x-forwarded-proto', 'x-forwarded-port', 'x-forwarded-prefix']
  ```
- **Monolog syslog** — routes logs to Zerops log collector:
  ```yaml
  # config/packages/monolog.yaml
  monolog:
      handlers:
          syslog:
              type: syslog
              level: debug
  ```
- **documentRoot: public** — required for Symfony's public directory structure
- **MAILER_DSN** — optional. If you have a `mailpit` service (hostname: mailpit), set `MAILER_DSN: smtp://mailpit:1025` for dev. Remove or change for stage
- **os: ubuntu** — on both build and run for full PHP extension compatibility

## Gotchas
- **No `.env` file** — Symfony auto-loads `.env` before the kernel. Even an empty key shadows the real OS env var. Delete all `.env*` files from the repo
- **`zsc execOnce` replaces lock bundle** — `sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php bin/console doctrine:migrations:migrate --no-interaction` handles concurrency. `marein/symfony-lock-doctrine-migrations-bundle` is NOT needed
- **No `--no-interaction` alone is not enough** — bare `php bin/console doctrine:migrations:migrate --no-interaction` runs on every container start. In HA mode, all containers run migrations simultaneously. Always wrap with `zsc execOnce`
- **DATABASE_URL already contains dbName** — PostgreSQL `connectionString` is `postgresql://user:pass@host:port/dbName`. Do NOT append `/${db_dbName}` — results in a double-path URL
- **asset-map:compile needs APP_ENV=prod** — set `APP_ENV: prod` in build-time `envVariables`, not run-time. Asset compiler skips in dev mode
- **Valkey var is `connectionString`** — `${cache_connectionString}` gives `redis://hostname:6379`. Use `cache` (or your actual hostname), NOT `redis` unless that is your import hostname
- **twbs/bootstrap in require-dev** — must be in `require` (not `require-dev`) when using `composer install --no-dev`
- **symfonycasts/sass-bundle** — must be v0.5+ for Alpine Linux compatibility; use `os: ubuntu` on build to avoid Alpine-specific issues
- **Never use `php bin/console server:run`** — php-nginx has a built-in web server on port 80
- **Cache warmup order** — run migrations before `cache:warmup`. The cache warmer may instantiate Doctrine repositories that require the schema to exist
