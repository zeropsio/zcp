# Symfony on Zerops
Symfony with PostgreSQL, Redis sessions, Doctrine migrations, and asset compilation.

## Keywords
symfony, php, postgresql, valkey, redis, doctrine, nginx, migrations, asset-map

## TL;DR
Symfony with PHP-Nginx, PostgreSQL, and Valkey -- requires `marein/symfony-lock-doctrine-migrations-bundle` for idempotent migrations.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: php@8.3
      buildCommands:
        - composer install --optimize-autoloader --no-dev
        - php bin/console asset-map:compile
        - php bin/console cache:warmup
      deployFiles: ./
      cache:
        - vendor
        - composer.lock
      envVariables:
        APP_ENV: prod
    deploy:
      readinessCheck:
        httpGet:
          port: 80
          path: /
    run:
      base: php-nginx@8.3
      documentRoot: public
      envVariables:
        APP_ENV: prod
        TRUSTED_PROXIES: 127.0.0.1,10.0.0.0/8
        DATABASE_URL: ${db_connectionString}/${db_dbName}?serverVersion=16&charset=utf8
        REDIS_URL: ${redis_connectionString}
        MAILER_DSN: smtp://mailpit:1025
      initCommands:
        - php bin/console doctrine:migrations:migrate --no-interaction
      healthCheck:
        httpGet:
          port: 80
          path: /
```

## import.yml
```yaml
#yamlPreprocessor=on
services:
  - hostname: app
    type: php-nginx@8.3
    enableSubdomainAccess: true
    envSecrets:
      APP_SECRET: <@generateRandomString(<32>)>

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: redis
    type: valkey@7.2
    mode: NON_HA
    priority: 10
```

## Configuration
- **TRUSTED_PROXIES: 127.0.0.1,10.0.0.0/8** -- required for Symfony behind Zerops reverse proxy
- **DATABASE_URL** uses `${db_connectionString}/${db_dbName}` cross-service reference with serverVersion parameter
- **REDIS_URL** uses `${redis_connectionString}` cross-service reference
- **APP_SECRET** is generated via `<@generateRandomString(<32>)>` in import.yml envSecrets
- **APP_ENV: prod** is set in both build (for asset compilation) and run environments
- **documentRoot: public** -- Symfony serves from the `public/` directory
- **MAILER_DSN** -- set to `smtp://mailpit:1025` for dev; change to production SMTP in production
- Configure Monolog syslog handler for Zerops log collection:
  ```yaml
  # config/packages/monolog.yaml
  monolog:
      handlers:
          syslog:
              type: syslog
              level: debug
  ```
- Configure Redis sessions in Symfony:
  ```yaml
  # config/packages/framework.yaml
  framework:
      session:
          handler_id: '%env(REDIS_URL)%'
  ```

## Common Failures
- **Migration lock errors** -- add `marein/symfony-lock-doctrine-migrations-bundle` for safe concurrent migrations
- **Assets 404** -- ensure `php bin/console asset-map:compile` runs in build (needs `APP_ENV: prod`)
- **twbs/bootstrap in require-dev** -- must be in `require` (not `require-dev`) when using `--no-dev` flag
- **Sass compilation fails on Alpine** -- ensure `symfonycasts/sass-bundle` is v0.5+ for Alpine compatibility

## Gotchas
- **marein/symfony-lock-doctrine-migrations-bundle** REQUIRED for idempotent Doctrine migrations in multi-container environments
- **twbs/bootstrap** must be in `require` (not `require-dev`) for `composer install --no-dev`
- **symfonycasts/sass-bundle** must be v0.5+ for Alpine Linux compatibility
- **Sessions** use Redis DB index 0 via `SESSION_HANDLER` config
- **documentRoot: public** is required for Symfony (serves from `public/` directory)
- 3 services: app + db (PostgreSQL) + redis (Valkey)
- **healthCheck is for stage/production only** — the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
