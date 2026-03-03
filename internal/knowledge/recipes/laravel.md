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
        APP_MAINTENANCE_DRIVER: file
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
        LOG_LEVEL: debug
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
        - php artisan migrate --isolated --force
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

        DB_CONNECTION: pgsql
        DB_DATABASE: db
        DB_HOST: db
        DB_USERNAME: ${db_user}
        DB_PASSWORD: ${db_password}
        DB_PORT: 5432

        LOG_CHANNEL: syslog
        LOG_LEVEL: debug
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
        - php artisan migrate --isolated --force
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

## Gotchas
- **Multi-base build** `[php, nodejs]` required only if project has npm/Vite assets (full setup)
- **league/flysystem-aws-s3-v3** must be in composer.json for S3 filesystem (full setup)
- **TRUSTED_PROXIES: "\*"** required for Laravel behind load balancer
- **os: ubuntu** on both build and run for full compatibility
- **Minimal setup** has no Redis/Valkey -- sessions, cache, and queue all use database driver
- **Minimal setup** has no Object Storage -- file uploads use local filesystem (not suitable for multi-container scaling)
- Full setup: 4 services (app + db + redis + storage). Minimal: 2 services (app + db).
- **healthCheck is for stage/production only** -- the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
