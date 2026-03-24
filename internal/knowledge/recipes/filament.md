# Filament on Zerops

## Keywords
filament, admin panel, laravel

## TL;DR
Filament is a Laravel admin panel. Use the Laravel recipe (`zerops_knowledge recipe=laravel`) for base Zerops config (APP_KEY, `${hostname_varName}` wiring, stack layers, dev/stage, scaffolding, gotchas). This recipe covers Filament-specific additions.

## zerops.yml
```yaml
zerops:
  - setup: appdev
    build:
      base: php-nginx@8.4
      os: ubuntu
      buildCommands:
        - composer install --optimize-autoloader --ignore-platform-reqs
      deployFiles: ./
      cache: [vendor, composer.lock]
    run:
      base: php-nginx@8.4
      os: ubuntu
      documentRoot: public
      envVariables:
        APP_NAME: MyFilamentApp
        APP_ENV: local
        APP_DEBUG: "true"
        APP_URL: ${zeropsSubdomain}

        DB_CONNECTION: pgsql
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_DATABASE: ${db_dbName}
        DB_USERNAME: ${db_user}
        DB_PASSWORD: ${db_password}

        REDIS_CLIENT: phpredis
        REDIS_HOST: ${cache_hostname}
        REDIS_PORT: ${cache_port}
        SESSION_DRIVER: redis
        CACHE_STORE: redis
        QUEUE_CONNECTION: redis

        FILESYSTEM_DISK: s3
        FILAMENT_FILESYSTEM_DISK: s3
        AWS_ACCESS_KEY_ID: ${storage_accessKeyId}
        AWS_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        AWS_BUCKET: ${storage_bucketName}
        AWS_ENDPOINT: ${storage_apiUrl}
        AWS_URL: ${storage_apiUrl}/${storage_bucketName}
        AWS_USE_PATH_STYLE_ENDPOINT: "true"
        AWS_DEFAULT_REGION: us-east-1

        LOG_CHANNEL: syslog
        LOG_LEVEL: debug
        TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"
      initCommands:
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --force
        - sudo -E -u zerops -- zsc execOnce initialize -- php artisan db:seed --force
```

## import.yml
```yaml
services:
  - hostname: appdev
    type: php-nginx@8.4
    startWithoutCode: true
    maxContainers: 1
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10
```

Filament can work with just **app + DB** (use `CACHE_STORE: database`, `SESSION_DRIVER: database`, drop Valkey and S3 blocks). Add layers as needed — see Laravel recipe for the layer-by-layer pattern.

## Filament-Specific Notes

- **`FILAMENT_FILESYSTEM_DISK: s3`** — must be set separately from `FILESYSTEM_DISK`. Filament panel uploads use their own disk config. Without it, media falls back to local disk (lost on deploy).
- **`zsc execOnce initialize -- php artisan db:seed`** — static key, runs once ever. Seeds Filament roles and initial data.
- **`objectStoragePolicy: public-read`** — required for uploaded media to be publicly accessible.
- **Scaffolding** (after Laravel base): `composer require filament/filament && php artisan filament:install --panels`
- All Laravel gotchas apply (APP_KEY project-level, no .env, no --isolated, Valkey var is `hostname` not `host`).

## Gotchas
- **`FILAMENT_FILESYSTEM_DISK`** must be set alongside `FILESYSTEM_DISK` — both to `s3`
- **`zsc execOnce initialize`** — if seeder fails, reset the key in Zerops GUI to re-run
- **No `.env` file** — scaffold with `composer create-project --no-scripts`
- **No `--isolated` on migrate** — `zsc execOnce` handles concurrency
- **Never use `php artisan serve`** — php-nginx has built-in web server on port 80
