# Laravel Jetstream on Zerops
Laravel with Inertia.js frontend. Requires multi-base build for npm assets.

## Keywords
laravel, jetstream, php, inertia, postgresql, valkey, redis, s3, nginx

## TL;DR
Laravel Jetstream with multi-base build `[php, nodejs]` for Inertia.js assets -- S3 filesystem, Valkey sessions/cache, PostgreSQL database.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base:
        - php@8.4
        - nodejs@18
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
      siteConfigPath: site.conf.tmpl
      envVariables:
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
        AWS_REGION: us-east-1
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

## import.yml
```yaml
#yamlPreprocessor=on
services:
  - hostname: app
    type: php-nginx@8.4
    enableSubdomainAccess: true
    envSecrets:
      APP_NAME: ZeropsLaravelJetstream
      APP_DEBUG: true
      APP_ENV: development
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

## Configuration
- **TRUSTED_PROXIES: "\*"** -- required for Laravel behind Zerops load balancer
- **LOG_CHANNEL: syslog** -- routes logs to Zerops log collector
- **SESSION_DRIVER / CACHE_STORE: redis** -- use Valkey instead of file-based sessions
- **FILESYSTEM_DISK: s3** -- use Zerops Object Storage for file uploads
- **AWS_USE_PATH_STYLE_ENDPOINT: true** -- required for Zerops S3-compatible storage
- **APP_KEY** is generated via `<@generateRandomString(<32>)>` in import.yml envSecrets
- **DB_PASSWORD / DB_USERNAME** are auto-injected via `${db_password}` / `${db_user}` cross-service refs
- **siteConfigPath: site.conf.tmpl** -- custom nginx config for Laravel (must exist in repo root)

## Common Failures
- **S3 driver not found** -- add `league/flysystem-aws-s3-v3` to composer.json
- **502 after deploy** -- ensure `enableSubdomainAccess: true` is set and subdomain is activated after first deploy
- **Session not persisting** -- verify `REDIS_HOST: redis` matches the Valkey service hostname
- **Assets not loading** -- confirm multi-base build includes `nodejs@18` and `npm run build` completes

## Gotchas
- **Multi-base build** `[php, nodejs]` required for Inertia.js/npm assets
- **league/flysystem-aws-s3-v3** must be in composer.json for S3 filesystem
- **TRUSTED_PROXIES: "\*"** required for Laravel behind load balancer
- **os: ubuntu** on both build and run for full compatibility
- **siteConfigPath: site.conf.tmpl** must exist in repo root with proper nginx config for Laravel
- 4 services: app + db (PostgreSQL) + redis (Valkey) + storage (Object Storage)
