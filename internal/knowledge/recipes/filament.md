# Filament (Laravel) on Zerops

Filament admin panel with PostgreSQL, Redis cache/sessions, S3 filesystem. Laravel 11 health checks.

## Keywords
filament, laravel, php, postgresql, valkey, redis, s3, admin panel

## TL;DR
Filament (Laravel) admin with PHP-Nginx, PostgreSQL, Valkey, and S3 — health check via `/up` endpoint.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: php@8.3
      buildCommands:
        - composer install --ignore-platform-reqs
    run:
      base: php-nginx@8.3
      siteConfigPath: site.conf.tmpl
      envVariables:
        SESSION_DRIVER: redis
        CACHE_STORE: redis
        FILESYSTEM_DISK: s3
        FILAMENT_FILESYSTEM_DISK: s3
        AWS_USE_PATH_STYLE_ENDPOINT: true
        REDIS_HOST: redis
      initCommands:
        - php artisan view:cache
        - php artisan config:cache
        - php artisan route:cache
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --isolated --force
        - sudo -E -u zerops -- zsc execOnce initialize -- php artisan db:seed --force
        - php artisan optimize
      readinessCheck:
        httpGet:
          port: 80
          path: /up
```

## import.yml
```yaml
services:
  - hostname: app
    type: php-nginx@8.3
    enableSubdomainAccess: true

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
    priority: 10
```

## Gotchas
- **league/flysystem-aws-s3-v3** required in composer.json for S3
- Health checks enabled out of box in Laravel 11 (/up endpoint)
- Sessions and cache in Redis, files in S3
- 5 services: app + pg + valkey + s3 + mailpit
