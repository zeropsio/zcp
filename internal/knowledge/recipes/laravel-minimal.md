# Laravel Minimal on Zerops

Laravel without Jetstream. PostgreSQL database, no Redis or Object Storage.

## Keywords
laravel, php, postgresql, nginx, minimal, php-nginx

## TL;DR
Laravel minimal with PostgreSQL -- single PHP build, database-backed sessions/cache/queue. No Redis, no S3, no frontend build step.

## zerops.yml
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
      siteConfigPath: site.conf.tmpl
      envVariables:
        APP_LOCALE: en
        APP_FAKER_LOCALE: en_US
        APP_FALLBACK_LOCALE: en
        APP_MAINTENANCE_DRIVER: file
        APP_MAINTENANCE_STORE: database
        APP_TIMEZONE: UTC
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

## import.yml
```yaml
#yamlPreprocessor=on
services:
  - hostname: app
    type: php-nginx@8.4
    enableSubdomainAccess: true
    envSecrets:
      APP_NAME: LaravelMinimal
      APP_DEBUG: true
      APP_ENV: development
      APP_KEY: <@generateRandomString(<32>)>

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Configuration
- **TRUSTED_PROXIES: "\*"** -- required for Laravel behind Zerops load balancer
- **LOG_CHANNEL: syslog** -- routes logs to Zerops log collector
- **SESSION_DRIVER / CACHE_STORE / QUEUE_CONNECTION: database** -- uses PostgreSQL for everything (no Redis)
- **APP_KEY** is generated via `<@generateRandomString(<32>)>` in import.yml envSecrets
- **DB_PASSWORD / DB_USERNAME** are auto-injected via `${db_password}` / `${db_user}` cross-service refs
- **siteConfigPath: site.conf.tmpl** -- custom nginx config for Laravel (must exist in repo root)

## Common Failures
- **502 after deploy** -- ensure `enableSubdomainAccess: true` is set and subdomain is activated after first deploy
- **Migration fails** -- verify `DB_HOST: db` matches the PostgreSQL service hostname
- **Assets not loading** -- check `APP_URL` is set to `${zeropsSubdomain}` for correct URL generation

## Gotchas
- **No Redis/Valkey** -- sessions, cache, and queue all use database driver (unlike laravel-jetstream)
- **No Object Storage** -- file uploads use local filesystem (not suitable for multi-container scaling)
- **TRUSTED_PROXIES: "\*"** required for Laravel behind load balancer
- **os: ubuntu** on both build and run for full compatibility
- **siteConfigPath: site.conf.tmpl** must exist in repo root with proper nginx config for Laravel
- 2 services: app (PHP-Nginx) + db (PostgreSQL)
