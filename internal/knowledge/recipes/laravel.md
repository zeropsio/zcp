# Laravel on Zerops
Laravel with PostgreSQL. Optional: Valkey for sessions/cache, Object Storage for S3 filesystem, multi-base build for npm/Vite assets.

## Keywords
laravel, php, postgresql, valkey, redis, s3, nginx, inertia, jetstream, php-nginx

## TL;DR
Laravel on PHP-Nginx with PostgreSQL -- add Valkey for Redis sessions/cache and Object Storage for S3 filesystem as needed. Multi-base build `[php, nodejs]` only if project has npm/Vite assets.

## Setup Options

| Setup | Services | Use When |
|-------|----------|----------|
| **Minimal** | app + db | No Redis, no S3, no frontend build. Database sessions/cache/queue. |
| **Full** | app + db + redis + storage | Redis sessions/cache, S3 filesystem, npm/Vite assets. |

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base:
        - php@8.4
        - nodejs@22
      os: ubuntu
      buildCommands:
        - composer install --optimize-autoloader --no-dev
        - npm install
        - npm run build
      deployFiles: ./
      cache:
        - vendor
        - composer.lock
        - node_modules
        - package-lock.json
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
        APP_NAME: MyLaravelApp
        APP_ENV: production
        APP_DEBUG: "false"
        APP_LOCALE: en
        APP_FAKER_LOCALE: en_US
        APP_FALLBACK_LOCALE: en
        APP_MAINTENANCE_DRIVER: cache
        APP_MAINTENANCE_STORE: database
        APP_TIMEZONE: UTC
        APP_URL: ${zeropsSubdomain}
        ASSET_URL: ${APP_URL}
        VITE_APP_NAME: ${APP_NAME}

        DB_CONNECTION: pgsql
        DB_DATABASE: db
        DB_HOST: db
        DB_PASSWORD: ${db_password}
        DB_PORT: 5432
        DB_USERNAME: ${db_user}

        AWS_ACCESS_KEY_ID: ${storage_accessKeyId}
        AWS_DEFAULT_REGION: us-east-1
        AWS_BUCKET: ${storage_bucketName}
        AWS_ENDPOINT: ${storage_apiUrl}
        AWS_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        AWS_URL: ${storage_apiUrl}/${storage_bucketName}
        AWS_USE_PATH_STYLE_ENDPOINT: true

        LOG_CHANNEL: syslog
        LOG_LEVEL: info
        LOG_STACK: single

        BROADCAST_CONNECTION: redis
        CACHE_PREFIX: cache
        CACHE_STORE: redis
        QUEUE_CONNECTION: redis
        REDIS_CLIENT: phpredis
        REDIS_HOST: redis
        REDIS_PORT: 6379
        SESSION_DRIVER: redis
        SESSION_ENCRYPT: false
        SESSION_LIFETIME: 120
        SESSION_PATH: /

        BCRYPT_ROUNDS: 12
        TRUSTED_PROXIES: "*"
        FILESYSTEM_DISK: s3
      initCommands:
        - php artisan view:cache
        - php artisan config:cache
        - php artisan route:cache
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --isolated --force
        - php artisan optimize
      healthCheck:
        httpGet:
          port: 80
          path: /up
```

## zerops.yml (Minimal)
```yaml
zerops:
  - setup: app
    build:
      base: php@8.4
      os: ubuntu
      buildCommands:
        - composer install --optimize-autoloader --no-dev
      deployFiles: ./
      cache:
        - vendor
        - composer.lock
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
        APP_LOCALE: en
        APP_FAKER_LOCALE: en_US
        APP_FALLBACK_LOCALE: en
        APP_MAINTENANCE_DRIVER: file
        APP_MAINTENANCE_STORE: database
        APP_TIMEZONE: UTC
        APP_NAME: MyLaravelApp
        APP_ENV: production
        APP_DEBUG: "false"
        APP_URL: ${zeropsSubdomain}
        ASSET_URL: ${APP_URL}
        VITE_APP_NAME: ${APP_NAME}

        DB_CONNECTION: pgsql
        DB_DATABASE: db
        DB_HOST: db
        DB_USERNAME: ${db_user}
        DB_PASSWORD: ${db_password}
        DB_PORT: 5432

        LOG_CHANNEL: syslog
        LOG_LEVEL: info
        LOG_STACK: single

        SESSION_DRIVER: database
        SESSION_LIFETIME: 120
        SESSION_ENCRYPT: false
        SESSION_PATH: /

        BROADCAST_CONNECTION: log
        FILESYSTEM_DISK: local
        QUEUE_CONNECTION: database
        CACHE_STORE: database

        TRUSTED_PROXIES: "*"
      initCommands:
        - php artisan view:cache
        - php artisan config:cache
        - php artisan route:cache
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --isolated --force
        - php artisan optimize
      healthCheck:
        httpGet:
          port: 80
          path: /up
```

## import.yml (Full)
```yaml
#yamlPreprocessor=on
services:
  - hostname: app
    type: php-nginx@8.4
    enableSubdomainAccess: true
    envSecrets:
      APP_KEY: <@generateRandomString(<32>)>

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: redis
    type: valkey@7.2
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10
```

## import.yml (Minimal)
```yaml
#yamlPreprocessor=on
services:
  - hostname: app
    type: php-nginx@8.4
    enableSubdomainAccess: true
    envSecrets:
      APP_KEY: <@generateRandomString(<32>)>

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Scaffolding

Fresh project setup on a Zerops container (bootstrap or manual):

```bash
zsc scale ram 1GiB 10m
composer create-project laravel/laravel . --no-scripts
composer run post-autoload-dump
rm -f .env.example
php artisan migrate --force
zsc scale ram auto
```

- `zsc scale ram 1GiB 10m` temporarily bumps container RAM to 1 GB for composer (default 128 MB causes OOM). `zsc scale ram auto` returns to autoscaling after scaffolding.
- `--no-scripts` skips all post-create hooks: no `.env` file created, no `database.sqlite`, no `key:generate`. Without this flag, Laravel creates `.env` with empty `APP_KEY=` which **shadows the valid OS env var from envSecrets** — breaking encryption at runtime.
- `post-autoload-dump` triggers package discovery (the only useful post-install hook).
- `rm .env.example` removes the template file (not needed — Zerops uses OS env vars exclusively).
- `php artisan migrate --force` creates required tables (sessions, cache, jobs, etc.) that Laravel needs at runtime. Without them, requests hitting cache/session middleware crash with "Undefined table."

## Configuration
- **TRUSTED_PROXIES: "\*"** -- required for Laravel behind Zerops load balancer
- **LOG_CHANNEL: syslog** -- routes logs to Zerops log collector
- **documentRoot: public** -- serves Laravel from the `public/` directory
- **APP_KEY** is generated via `<@generateRandomString(<32>)>` in import.yml envSecrets
- **DB_PASSWORD / DB_USERNAME** are auto-injected via `${db_password}` / `${db_user}` cross-service refs
- **Full setup**: `SESSION_DRIVER / CACHE_STORE: redis` -- use Valkey; `FILESYSTEM_DISK: s3` -- use Object Storage
- **Minimal setup**: `SESSION_DRIVER / CACHE_STORE / QUEUE_CONNECTION: database` -- uses PostgreSQL for everything (no Redis)
- **AWS_USE_PATH_STYLE_ENDPOINT: true** -- required for Zerops S3-compatible storage (full setup only)

## Common Failures
- **S3 driver not found** -- add `league/flysystem-aws-s3-v3` to composer.json (full setup only)
- **502 after deploy** -- ensure `enableSubdomainAccess: true` is set and subdomain is activated after first deploy
- **Session not persisting** -- verify `REDIS_HOST: redis` matches the Valkey service hostname (full setup) or `SESSION_DRIVER: database` is set (minimal setup)
- **Assets not loading** -- confirm multi-base build includes `nodejs@22` and `npm run build` completes (full setup)
- **Migration fails** -- verify `DB_HOST: db` matches the PostgreSQL service hostname
- **APP_KEY empty / encryption broken** -- if `.env` exists with empty `APP_KEY=`, it shadows the valid OS env var from envSecrets. Cause: `composer create-project` was run without `--no-scripts`. Fix: delete `.env` or scaffold with `--no-scripts`.
- **Data lost after redeploy** -- SQLite database is destroyed when the container rebuilds. Switch to a database service (PostgreSQL or MariaDB).
- **HTTP 500 — check logs FIRST** -- read `{mountPath}/storage/logs/laravel.log` and `zerops_logs`. The log tells you the exact error. Do not guess.
- **Never use `php artisan serve`** -- php-nginx has built-in web server on port 80. `artisan serve` bypasses nginx config, documentRoot, and rewrite rules.

## Gotchas
- **Multi-base build** `[php, nodejs]` required only if project has npm/Vite assets (full setup)
- **league/flysystem-aws-s3-v3** must be in composer.json for S3 filesystem (full setup)
- **TRUSTED_PROXIES: "\*"** required for Laravel behind load balancer
- **os: ubuntu** on both build and run for full compatibility
- **Minimal setup** has no Redis/Valkey -- sessions, cache, and queue all use database driver
- **Minimal setup** has no Object Storage -- file uploads use local filesystem (not suitable for multi-container scaling)
- Full setup: 4 services (app + db + redis + storage). Minimal: 2 services (app + db).
- **No `.env` file** — Zerops injects all variables as OS env vars. Scaffold with `composer create-project laravel/laravel . --no-scripts` — this prevents `.env` and `database.sqlite` from being created. Without `--no-scripts`, `.env` is created with empty `APP_KEY=` which **shadows the valid OS env var from envSecrets**, breaking encryption at runtime. After scaffolding: `composer run post-autoload-dump` for package discovery, then `rm -f .env.example`.
- **No SQLite** — Laravel defaults to SQLite but containers are volatile (data lost on redeploy). Always use a database service (PostgreSQL preferred, or MariaDB). SQLite is acceptable only for PHPUnit test suites.
