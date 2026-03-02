# Filament (Laravel) on Zerops
Filament admin panel on Laravel with PostgreSQL, Redis cache/sessions, S3 filesystem.

## Keywords
filament, laravel, php, postgresql, valkey, redis, s3, admin panel, nginx

## TL;DR
Filament (Laravel) admin panel with PHP-Nginx, PostgreSQL, Valkey, and S3 -- health check via `/up` endpoint.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: php@8.3
      buildCommands:
        - composer install --ignore-platform-reqs
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
      base: php-nginx@8.3
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

        AWS_ACCESS_KEY_ID: ${storage_accessKeyId}
        AWS_REGION: us-east-1
        AWS_BUCKET: ${storage_bucketName}
        AWS_ENDPOINT: ${storage_apiUrl}
        AWS_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        AWS_URL: ${storage_apiUrl}/${storage_bucketName}
        AWS_USE_PATH_STYLE_ENDPOINT: true

        BCRYPT_ROUNDS: 12
        FILESYSTEM_DISK: s3
        FILAMENT_FILESYSTEM_DISK: s3
      initCommands:
        - php artisan view:cache
        - php artisan config:cache
        - php artisan route:cache
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --isolated --force
        - sudo -E -u zerops -- zsc execOnce initialize -- php artisan db:seed --force
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
    type: php-nginx@8.3
    enableSubdomainAccess: true
    envSecrets:
      APP_KEY: <@generateRandomString(<32>)>
      APP_DEBUG: true
      APP_ENV: development

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

## Configuration
- **TRUSTED_PROXIES** -- not set by default in Filament recipe; add `TRUSTED_PROXIES: "*"` if behind Zerops L7 balancer
- **LOG_CHANNEL: syslog** -- routes logs to Zerops log collector
- **FILAMENT_FILESYSTEM_DISK: s3** -- Filament-specific S3 configuration for media uploads
- **SESSION_DRIVER / CACHE_STORE: redis** -- use Valkey instead of file-based sessions
- **APP_KEY** is generated via `<@generateRandomString(<32>)>` in import.yml envSecrets
- **siteConfigPath: site.conf.tmpl** -- custom nginx config (must exist in repo root)

## Common Failures
- **S3 driver not found** -- add `league/flysystem-aws-s3-v3` to composer.json
- **502 after deploy** -- ensure subdomain is activated after first deploy
- **Filament media not displaying** -- verify `FILAMENT_FILESYSTEM_DISK: s3` and `objectStoragePolicy: public-read`
- **Migration conflicts on scale-up** -- `zsc execOnce ${appVersionId}` ensures migrations run only once per deploy version

## Gotchas
- **league/flysystem-aws-s3-v3** required in composer.json for S3
- **Health checks** enabled out of the box in Laravel 11 (`/up` endpoint)
- **zsc execOnce ${appVersionId}** ensures migrations run exactly once per deploy across all containers
- **zsc execOnce initialize** runs db:seed only once ever (static key, not per-deploy)
- **FILAMENT_FILESYSTEM_DISK** must be set separately from `FILESYSTEM_DISK` for Filament panel uploads
- 4 services: app + db (PostgreSQL) + redis (Valkey) + storage (Object Storage)
