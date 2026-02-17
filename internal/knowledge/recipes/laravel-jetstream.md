# Laravel Jetstream on Zerops

Laravel with Inertia.js frontend. Requires multi-base build for npm assets.

## Keywords
laravel, jetstream, php, inertia, postgresql, valkey, redis, s3

## TL;DR
Laravel Jetstream with multi-base build `[php, nodejs]` for Inertia.js assets — S3 filesystem and Valkey sessions.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base:
        - php@8.4
        - nodejs@18
      buildCommands:
        - composer install --optimize-autoloader --no-dev
        - npm install
        - npm run build
    run:
      base: php-nginx@8.4
      envVariables:
        FILESYSTEM_DISK: s3
        SESSION_DRIVER: redis
        CACHE_STORE: redis
        TRUSTED_PROXIES: "*"
        AWS_USE_PATH_STYLE_ENDPOINT: true
      initCommands:
        - php artisan migrate --isolated --force
        - php artisan cache:clear
        - php artisan config:cache
```

## import.yml
```yaml
services:
  - hostname: app
    type: php-nginx@8.4
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
- **Multi-base build** [php, nodejs] required for Inertia.js/npm assets
- **league/flysystem-aws-s3-v3** required in composer.json for S3
- **TRUSTED_PROXIES: "\*"** required for Laravel behind load balancer
- 5 services: app + pg + redis + s3 + mailpit
