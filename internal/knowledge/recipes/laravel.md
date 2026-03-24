# Laravel on Zerops

PHP-Nginx runtime with managed services. Build your stack layer by layer — start with just the app, add database, cache, storage as needed.

## Keywords
laravel, php, php-nginx, postgresql, mariadb, valkey, redis, s3, object-storage, zerops.yml, documentRoot, artisan, composer

## TL;DR
PHP-Nginx, `documentRoot: public`, port 80 fixed. APP_KEY on project level (`base64:` + 32 bytes). Wire services with `${hostname_varName}` refs. Scaffold: `composer create-project --no-scripts`. No `.env` files.

## APP_KEY

Must be **project-level** (shared across dev+stage). Laravel requires `base64:` prefix with 32 random bytes base64-encoded.

**Generate**: `php -r "echo 'base64:'.base64_encode(random_bytes(32)).\"\\n\";"`
**Set**: `zerops_env project=true variables=["APP_KEY=base64:..."]`

Do NOT use `envSecrets` in import.yml — generates per-service key (dev and stage get different keys, breaking encryption). Do NOT use `<@generateRandomString(<32>)>` — produces plaintext, not the required `base64:` format.

## Wiring Managed Services

Cross-service pattern: `${hostname_varName}` — resolved at container start from the target service's env vars.

After adding any service: `zerops_discover includeEnvs=true` to see available vars. Map ONLY discovered vars — guessing names causes silent failures (unresolved refs stay as literal strings).

### Service vars reference

| Type | hostname | port | user | password | dbName | connectionString | Auth extras |
|------|:---:|:---:|:---:|:---:|:---:|:---:|-------------|
| PostgreSQL | yes | 5432 | yes | yes | yes | `postgresql://...` | superUser, superUserPassword |
| MariaDB | yes | 3306 | yes* | yes | yes* | `mysql://...` | *user and dbName = service hostname |
| Valkey/KeyDB | yes | 6379 | — | — | — | `redis://...` | No auth (private network) |
| Object Storage | yes | — | — | — | — | — | accessKeyId, secretAccessKey, apiUrl, bucketName |
| Meilisearch | yes | 7700 | — | — | — | `http://...` | masterKey, defaultSearchKey, defaultAdminKey |
| Elasticsearch | yes | 9200 | yes | yes | — | `http://...` | — |
| Typesense | yes | 8108 | — | — | — | `http://...` | apiKey |
| ClickHouse | yes | 9000 | yes | yes | yes | `clickhouse://...` | portHttp (8123) |
| Qdrant | yes | 6333 | — | — | — | `http://...` | apiKey, grpcPort |
| NATS | yes | 4222 | yes | yes | — | `nats://...` | JET_STREAM_ENABLED |
| Kafka | yes | 9092 | yes | yes | — | — | — |

All types expose `hostname`, `port`, `connectionString` (except Object Storage and Kafka). The pattern `${X_hostname}`, `${X_port}` works universally — `X` is your service hostname from import.yml.

## Stack Layers

Build your stack incrementally. Each layer shows what to **add** to import.yml and zerops.yml.

### Layer 0: Just Laravel (no managed services)

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
        - composer install --ignore-platform-reqs
      deployFiles:
        - vendor
        - .
    run:
      base: php-nginx@8.4
      os: ubuntu
      documentRoot: public
      envVariables:
        APP_NAME: MyApp
        APP_ENV: local
        APP_DEBUG: "true"
        APP_URL: ${zeropsSubdomain}
        LOG_CHANNEL: syslog
        LOG_LEVEL: debug
        SESSION_DRIVER: file
        CACHE_STORE: file
        QUEUE_CONNECTION: sync
        TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"
```

Volatile filesystem — sessions/cache lost on every deploy. OK for stateless APIs.

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
        DB_CONNECTION: pgsql           # or mysql for MariaDB
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_DATABASE: ${db_dbName}
        DB_USERNAME: ${db_user}
        DB_PASSWORD: ${db_password}
        SESSION_DRIVER: database       # upgrade from file
        CACHE_STORE: database          # upgrade from file
        QUEUE_CONNECTION: database     # upgrade from sync
```

**Add initCommands:**
```yaml
      initCommands:
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --force
```

Sessions, cache, jobs now survive deploys. Switch `DB_CONNECTION` and types — the `${db_*}` refs work for both PostgreSQL and MariaDB.

### Layer 2: + Cache (Valkey)

In-memory sessions/cache — faster than database driver.

**Add to import.yml:**
```yaml
  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10
```

**Add/change in zerops.yml envVariables:**
```yaml
        REDIS_CLIENT: phpredis         # pre-installed in php-nginx (not predis)
        REDIS_HOST: ${cache_hostname}  # NOTE: "hostname", NOT "host"
        REDIS_PORT: ${cache_port}
        SESSION_DRIVER: redis          # upgrade from database
        CACHE_STORE: redis             # upgrade from database
        QUEUE_CONNECTION: redis        # upgrade from database
```

No password needed — Valkey runs on private network without auth.

### Layer 3: + File Storage (Object Storage)

S3-compatible storage for uploads, media. Persists across deploys.

**Add to import.yml:**
```yaml
  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10
```

**Add to zerops.yml envVariables:**
```yaml
        FILESYSTEM_DISK: s3
        AWS_ACCESS_KEY_ID: ${storage_accessKeyId}
        AWS_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        AWS_BUCKET: ${storage_bucketName}
        AWS_ENDPOINT: ${storage_apiUrl}
        AWS_URL: ${storage_apiUrl}/${storage_bucketName}
        AWS_USE_PATH_STYLE_ENDPOINT: "true"
        AWS_DEFAULT_REGION: us-east-1
```

**Requires**: `composer require league/flysystem-aws-s3-v3`

### Layer 4: + Search, Message Brokers, etc.

Same pattern for any managed service. After import:
1. `zerops_discover includeEnvs=true` — find available var names
2. Map in zerops.yml: `MY_VAR: ${hostname_varName}`
3. Install the PHP client package

**Meilisearch** (Laravel Scout):
```yaml
# import: hostname: search, type: meilisearch@1.10
        SCOUT_DRIVER: meilisearch
        MEILISEARCH_HOST: http://${search_hostname}:${search_port}
        MEILISEARCH_KEY: ${search_masterKey}
```
Requires: `composer require laravel/scout meilisearch/meilisearch-php`

**Elasticsearch**:
```yaml
# import: hostname: elastic, type: elasticsearch@8.16
        ELASTICSEARCH_HOST: ${elastic_hostname}
        ELASTICSEARCH_PORT: ${elastic_port}
        ELASTICSEARCH_USER: ${elastic_user}
        ELASTICSEARCH_PASSWORD: ${elastic_password}
```
Requires: `composer require elasticsearch/elasticsearch:^8.0` — client version MUST match server major version.

**Typesense** (alternative search):
```yaml
# import: hostname: typesense, type: typesense@27.1
        TYPESENSE_HOST: ${typesense_hostname}
        TYPESENSE_PORT: ${typesense_port}
        TYPESENSE_API_KEY: ${typesense_apiKey}
```

## Dev vs Stage zerops.yml

Managed services are **shared** — both dev and stage use the same `db`, `cache`, `storage`. Only Laravel-specific config differs:

| | Dev | Stage |
|---|-----|-------|
| `APP_ENV` | `local` | `production` |
| `APP_DEBUG` | `"true"` | `"false"` |
| `LOG_LEVEL` | `debug` | `info` |
| `initCommands` | migrate only | migrate + `php artisan optimize` |
| `healthCheck` | omit | `/up` |
| `readinessCheck` | omit | `/up` |
| `build.base` | `php@8.4` | `[php@8.4, nodejs@22]` if assets |
| `buildCommands` | `composer install --ignore-platform-reqs` | + `npm install && npm run build` if assets |
| Service refs | `${db_hostname}`, `${cache_hostname}`, ... | **same** |

Stage zerops.yml additions (on top of dev):
```yaml
zerops:
  - setup: appstage
    deploy:
      readinessCheck:
        httpGet:
          port: 80
          path: /up
    run:
      base: php-nginx@8.4
      os: ubuntu
      documentRoot: public
      envVariables:
        APP_ENV: production
        APP_DEBUG: "false"
        LOG_LEVEL: info
      initCommands:
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --force
        - php artisan optimize
      healthCheck:
        httpGet:
          port: 80
          path: /up
```

`php artisan optimize` = config:cache + route:cache + view:cache + event:cache. Do NOT list them separately.

## Scaffolding

Fresh project setup on a Zerops dev container:

```bash
zsc scale ram 1GiB 10m
composer create-project laravel/laravel . --no-scripts
composer run post-autoload-dump
rm -f .env .env.example
php artisan migrate --force       # only if DB exists
zsc scale ram auto
```

- `zsc scale ram 1GiB 10m` — temporary RAM bump for composer (default 128 MB causes OOM)
- `--no-scripts` — prevents `.env` creation with empty `APP_KEY=` that **shadows the OS env var**
- `rm -f .env` — safety net; if `.env` exists with empty values, it shadows OS env vars for ALL keys
- `migrate --force` — creates sessions, cache, jobs tables needed by database drivers

## Configuration

- **Trusted proxies** — Laravel 11+ does not auto-read `TRUSTED_PROXIES` from env. Wire in `bootstrap/app.php`:
  ```php
  ->withMiddleware(function (Middleware $middleware) {
      $middleware->trustProxies(
          at: explode(',', env('TRUSTED_PROXIES', '127.0.0.1')),
      );
  })
  ```
- **LOG_CHANNEL: syslog** — routes logs to Zerops log collector
- **documentRoot: public** — required for Laravel's public directory structure
- **phpredis** — pre-installed in php-nginx image. Use `REDIS_CLIENT: phpredis` (not `predis`)
- **os: ubuntu** — on both build and run for full PHP extension compatibility

## Gotchas
- **No `.env` file** — scaffold with `--no-scripts`. Empty `.env` values shadow valid OS env vars, breaking APP_KEY and everything else
- **No SQLite** — container filesystem replaced on deploy. Always use a database service
- **No `--isolated` on migrate** — requires a working cache store. With `CACHE_STORE=database`, tries to lock via cache table that doesn't exist yet (chicken-and-egg). `zsc execOnce` already handles concurrency
- **Config cache after env changes** — after `zerops_env`, run `php artisan config:clear` on the container. `optimize` caches config at deploy time; changed env vars need a cache clear
- **Elasticsearch client version** — must match server major version. ES 8.x server needs `elasticsearch/elasticsearch:^8.0`, not v9 (incompatible version header)
- **Valkey var is `hostname`** — use `${cache_hostname}`, NOT `${cache_host}`. `host` does not exist as a Valkey env var
- **MariaDB user/dbName = hostname** — not static like PostgreSQL's `db`. Always use `${db_user}`, `${db_dbName}` dynamic refs
- **`league/flysystem-aws-s3-v3`** — required in composer.json for S3 filesystem
- **`AWS_USE_PATH_STYLE_ENDPOINT: "true"`** — required for Zerops S3 (MinIO backend)
- **Multi-base build** `[php@8.4, nodejs@22]` — only if project has npm/Vite assets
- **Never use `php artisan serve`** — php-nginx has built-in web server on port 80
