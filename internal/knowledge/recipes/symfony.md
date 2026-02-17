# Symfony on Zerops

Symfony with PostgreSQL, Redis sessions, Doctrine migrations. Requires lock bundle for idempotent migrations.

## Keywords
symfony, php, postgresql, valkey, redis, doctrine, nginx, migrations

## TL;DR
Symfony with PHP-Nginx, PostgreSQL, and Valkey — requires `marein/symfony-lock-doctrine-migrations-bundle` for idempotent migrations.

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
    run:
      base: php-nginx@8.3
      envVariables:
        APP_ENV: prod
        TRUSTED_PROXIES: 127.0.0.1,10.0.0.0/8
        DATABASE_URL: ${db_connectionString}/${db_dbName}?serverVersion=16&charset=utf8
        REDIS_URL: ${redis_connectionString}
      initCommands:
        - php bin/console doctrine:migrations:migrate
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

## composer.json (REQUIRED)
```json
{
  "require": {
    "marein/symfony-lock-doctrine-migrations-bundle": "^1.0"
  }
}
```

## Gotchas
- **marein/symfony-lock-doctrine-migrations-bundle** REQUIRED for idempotent migrations in containers
- Sessions use Redis DB index 0 (SESSION_HANDLER config)
- **twbs/bootstrap** must be in require (not require-dev) for --no-dev
- **symfonycasts/sass-bundle** must be v0.5+ for Alpine compatibility
