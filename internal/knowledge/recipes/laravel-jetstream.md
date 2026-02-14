# Laravel Jetstream on Zerops

Laravel with Inertia.js frontend. Requires multi-base build for npm assets.

## zerops.yml (multi-base build)
```yaml
zerops:
  - setup: app
    build:
      base: [php@8.4, nodejs@18]  # Multi-base for frontend assets
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
        TRUSTED_PROXIES: "*"  # For reverse proxy
        AWS_USE_PATH_STYLE_ENDPOINT: true
      initCommands:
        - php artisan migrate --isolated --force
        - php artisan cache:clear
        - php artisan config:cache
```

## Gotchas
- **Multi-base build** [php, nodejs] required for Inertia.js/npm assets
- **league/flysystem-aws-s3-v3** required in composer.json for S3
- **TRUSTED_PROXIES: "*"** required for Laravel behind load balancer
- 5 services: app + pg + redis + s3 + mailpit
