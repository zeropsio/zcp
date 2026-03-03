# Laravel on Zerops
Laravel with PostgreSQL. Optional: Valkey for sessions/cache, Object Storage for S3 filesystem, multi-base build for npm/Vite assets.

## Keywords
laravel, php, postgresql, valkey, redis, s3, nginx, inertia, jetstream, php-nginx

## TL;DR
Laravel on PHP-Nginx with PostgreSQL -- add Valkey for Redis sessions/cache and Object Storage for S3 filesystem as needed. Multi-base build `[php, nodejs]` only if project has npm/Vite assets.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base:
        - php@8.4
        - nodejs@22  # remove nodejs line if no npm/Vite assets
      os: ubuntu
      buildCommands:
        - composer install --optimize-autoloader --no-dev
        - npm install       # remove if no npm/Vite assets
        - npm run build     # remove if no npm/Vite assets
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
        APP_URL: ${zeropsSubdomain}

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
        LOG_LEVEL: info

        CACHE_STORE: redis
        QUEUE_CONNECTION: redis
        REDIS_CLIENT: phpredis
        REDIS_HOST: redis
        SESSION_DRIVER: redis

        TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"
        FILESYSTEM_DISK: s3
      initCommands:
        - php artisan view:cache
        - php artisan config:cache
        - php artisan route:cache
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --isolated --force
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

## Scaffolding

Fresh project setup on a Zerops container (bootstrap or manual):

```bash
zsc scale ram 1GiB 10m
composer create-project laravel/laravel . --no-scripts
composer run post-autoload-dump
rm -f .env.example
php artisan migrate --force
zsc scale ram auto
```

- `zsc scale ram 1GiB 10m` temporarily bumps RAM to 1 GB for composer (default 128 MB causes OOM). `zsc scale ram auto` returns to autoscaling after.
- `--no-scripts` prevents `.env` creation with empty `APP_KEY=` that **shadows the valid OS env var from envSecrets** — breaking encryption at runtime.
- `post-autoload-dump` triggers package discovery (the only useful post-install hook).
- `php artisan migrate --force` creates required tables (sessions, cache, jobs) that Laravel needs at runtime.

## Configuration

- **Trusted proxies** — Laravel 11+ does not auto-read `TRUSTED_PROXIES` from env. Wire it in `bootstrap/app.php`:
  ```php
  ->withMiddleware(function (Middleware $middleware) {
      $middleware->trustProxies(
          at: explode(',', env('TRUSTED_PROXIES', '127.0.0.1')),
      );
  })
  ```
- **LOG_CHANNEL: syslog** — routes logs to Zerops log collector
- **documentRoot: public** — serves Laravel from the `public/` directory
- **APP_KEY** — generated via `<@generateRandomString(<32>)>` in import.yml envSecrets
- **DB credentials** — auto-injected via `${db_password}` / `${db_user}` cross-service refs
- **AWS_USE_PATH_STYLE_ENDPOINT: true** — required for Zerops S3-compatible storage
- **Without Redis**: set `SESSION_DRIVER`, `CACHE_STORE`, `QUEUE_CONNECTION` to `database`; remove AWS_* vars and storage service if no Object Storage

## Common Failures
- **S3 driver not found** — add `league/flysystem-aws-s3-v3` to composer.json
- **502 after deploy** — ensure `enableSubdomainAccess: true` is set and subdomain is activated after first deploy
- **Session not persisting** — verify `REDIS_HOST: redis` matches the Valkey service hostname, or use `SESSION_DRIVER: database`
- **Assets not loading** — multi-base build needs `nodejs@22` and `npm run build`
- **Migration fails** — verify `DB_HOST: db` matches the PostgreSQL service hostname
- **HTTP 500** — check `{mountPath}/storage/logs/laravel.log` and `zerops_logs` first. Do not guess.
- **Never use `php artisan serve`** — php-nginx has built-in web server on port 80

## Gotchas
- **No `.env` file** — scaffold with `--no-scripts` to prevent `.env` with empty `APP_KEY=` shadowing envSecrets
- **No SQLite** — containers are volatile (data lost on redeploy). Always use a database service.
- **Multi-base build** `[php, nodejs]` required only if project has npm/Vite assets
- **league/flysystem-aws-s3-v3** must be in composer.json for S3 filesystem
- **os: ubuntu** on both build and run for full compatibility
- Without Redis/Object Storage: 2 services (app + db). Full: 4 services (app + db + redis + storage).
