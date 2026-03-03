# Twill CMS on Zerops
Laravel Twill CMS with multi-base build, custom nginx, S3 media with Glide.

## Keywords
twill, laravel, php, cms, postgresql, valkey, redis, s3, nginx, glide

## TL;DR
Twill CMS with multi-base `[php, nodejs]` build -- Alpine build, Ubuntu runtime, Glide S3 media, custom nginx config.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base:
        - php@8.3
        - nodejs@22
      os: alpine
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
      base: php-nginx@8.3
      os: ubuntu
      documentRoot: public
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

        BCRYPT_ROUNDS: 12
        TRUSTED_PROXIES: "*"
        MEDIA_LIBRARY_ENDPOINT_TYPE: s3
        GLIDE_USE_SOURCE_DISK: s3

        AWS_ACCESS_KEY_ID: ${storage_accessKeyId}
        AWS_DEFAULT_REGION: us-east-1
        AWS_BUCKET: ${storage_bucketName}
        AWS_ENDPOINT: ${storage_apiUrl}
        AWS_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        AWS_URL: ${storage_apiUrl}/${storage_bucketName}
        AWS_USE_PATH_STYLE_ENDPOINT: true
      initCommands:
        - sudo -E -u zerops -- zsc execOnce initialize -- php artisan twill:install -n
        - sudo -E -u zerops -- zsc execOnce initializeadmin -- php artisan twill:superadmin twill@zerops.io zerops
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --isolated --force
        - sudo -E -u zerops -- zsc execOnce initializeSeed -- php artisan db:seed --force
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
- **TRUSTED_PROXIES: "\*"** -- required for Laravel behind Zerops load balancer
- **LOG_CHANNEL: syslog** -- routes logs to Zerops log collector
- **MEDIA_LIBRARY_ENDPOINT_TYPE: s3** -- Twill media library uses S3 backend
- **GLIDE_USE_SOURCE_DISK: s3** -- Glide image processing reads from S3, caches locally
- **AWS_USE_PATH_STYLE_ENDPOINT: true** -- required for Zerops S3-compatible storage
- **APP_KEY** is generated via `<@generateRandomString(<32>)>` in import.yml envSecrets
- **documentRoot: public** -- serves Laravel from the `public/` directory
- Default superadmin: twill@zerops.io / zerops (created via `zsc execOnce initializeadmin`)

## Common Failures
- **S3 driver not found** -- `area17/twill` bundles `league/flysystem-aws-s3-v3` as a transitive dependency; verify it is resolved in `composer.lock`
- **Twill install fails** -- `zsc execOnce initialize` runs `twill:install -n` only once; if it fails, reset the key in Zerops GUI
- **Media images broken** -- verify `GLIDE_USE_SOURCE_DISK: s3` and `objectStoragePolicy: public-read`
- **OS mismatch errors** -- Alpine build is fine for composer/npm, Ubuntu runtime needed for PHP extensions

## Gotchas
- **Multi-base build** `[php, nodejs]` required for frontend assets
- **OS mismatch**: Alpine build (faster) with Ubuntu runtime (compatibility)
- **zsc execOnce** with static keys (`initialize`, `initializeadmin`, `initializeSeed`) run once ever; `${appVersionId}` runs once per deploy
- **S3 filesystem** -- `area17/twill` bundles S3 support via `league/flysystem-aws-s3-v3` as a transitive dependency; no need to add it directly to composer.json
- **AWS_USE_PATH_STYLE_ENDPOINT: true** required for Zerops S3
- **Glide** uses S3 source disk with local caching for media transformations
- 4 services: app + db (PostgreSQL) + redis (Valkey) + storage (Object Storage)
- **healthCheck is for stage/production only** -- the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
