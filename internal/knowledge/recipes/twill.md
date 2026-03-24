# Twill CMS on Zerops

## Keywords
twill, cms, glide, media

## TL;DR
Twill is a Laravel CMS. Use the Laravel recipe (`zerops_knowledge recipe=laravel`) for base Zerops config (APP_KEY, `${hostname_varName}` wiring, dev/stage, scaffolding, gotchas). This recipe covers Twill-specific requirements.

## Minimum Stack

Twill requires: **app + DB + Valkey + Object Storage**. Unlike base Laravel, these are not optional — Twill needs Redis for cache and S3 for media.

## zerops.yml
```yaml
zerops:
  - setup: appdev
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
      cache: [vendor, composer.lock, node_modules, package-lock.json]
    run:
      base: php-nginx@8.4
      os: ubuntu
      documentRoot: public
      envVariables:
        APP_NAME: MyTwillApp
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
        MEDIA_LIBRARY_ENDPOINT_TYPE: s3
        GLIDE_USE_SOURCE_DISK: s3
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
        - sudo -E -u zerops -- zsc execOnce initialize -- php artisan twill:install -n
        - sudo -E -u zerops -- zsc execOnce initializeadmin -- php artisan twill:superadmin twill@zerops.io zerops
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --force
        - sudo -E -u zerops -- zsc execOnce initializeSeed -- php artisan db:seed --force
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

## Twill-Specific Notes

- **Multi-base build always required** — `[php@8.4, nodejs@22]`. Twill has frontend assets even in dev.
- **MEDIA_LIBRARY_ENDPOINT_TYPE + GLIDE_USE_SOURCE_DISK** — both `s3`. Without them, media goes to local disk (lost on deploy).
- **Static execOnce keys** — `initialize`, `initializeadmin`, `initializeSeed` run once ever (not per deploy). `${appVersionId}` runs once per deploy. Order matters: twill:install → migrate → seed.
- **Default superadmin**: twill@zerops.io / zerops
- **S3 driver bundled** — `area17/twill` includes `league/flysystem-aws-s3-v3` as transitive dep, no need to add it.
- **`objectStoragePolicy: public-read`** — required for media to be publicly accessible.
- **Glide** caches processed images locally — cache lost on deploy, rebuilds automatically.
- All Laravel gotchas apply (APP_KEY project-level, no .env, no --isolated, Valkey var is `hostname` not `host`).

## Gotchas
- **Multi-base build always required** — `[php@8.4, nodejs@22]` even in dev
- **No `--isolated` on migrate** — `zsc execOnce` handles concurrency
- **No `.env` file** — scaffold with `composer create-project --no-scripts`
- **`objectStoragePolicy: public-read`** — without it, uploaded media returns 403
- **Never use `php artisan serve`** — php-nginx has built-in web server on port 80
