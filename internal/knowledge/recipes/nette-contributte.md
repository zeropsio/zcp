# Nette with Contributte on Zerops

Nette framework using Contributte packages — Redis sessions, Monolog syslog logging, Doctrine migrations. See the base `nette` recipe for stack wiring and common gotchas. This recipe documents the Contributte-specific differences.

## Keywords
nette, contributte, monolog, doctrine, latte, tracy

## TL;DR
Nette + Contributte on PHP-Apache. Key differences from base Nette: flat DB env vars (not DSN), `SyslogHandler` for Zerops log collection, `addDynamicParameters` required in Bootstrap.php, fixtures init command for dev.

## zerops.yml

```yaml
zerops:
  - setup: app
    build:
      base: php@8.3
      os: alpine
      buildCommands:
        - composer install --optimize-autoloader
      deployFiles: ./
      cache:
        - vendor
        - composer.lock
    deploy:
      readinessCheck:
        httpGet:
          port: 80
          path: /
    run:
      base: php-apache@8.3
      os: alpine
      documentRoot: www/
      envVariables:
        NETTE_DEBUG: "1"
        NETTE_ENV: development
        DATABASE_HOSTNAME: ${db_hostname}
        DATABASE_PORT: ${db_port}
        DATABASE_USER: ${db_user}
        DATABASE_PASSWORD: ${db_password}
        DATABASE_NAME: ${db_dbName}
        REDIS_URI: tcp://${cache_hostname}:${cache_port}
      initCommands:
        - zsc execOnce ${appVersionId}-migration -- php /var/www/bin/console migrations:migrate --no-interaction --allow-no-migration
        - zsc execOnce ${appVersionId}-fixtures -- php /var/www/bin/console doctrine:fixtures:load --no-interaction
        - chown -R zerops:zerops /var/www/var/tmp/
      healthCheck:
        httpGet:
          port: 80
          path: /
```

Stage service zerops.yml — use `--no-dev` and remove fixtures initCommand:
```yaml
zerops:
  - setup: appstage
    build:
      base: php@8.3
      os: alpine
      buildCommands:
        - composer install --optimize-autoloader --no-dev
      deployFiles: ./
      cache:
        - vendor
        - composer.lock
    deploy:
      readinessCheck:
        httpGet:
          port: 80
          path: /
    run:
      base: php-apache@8.3
      os: alpine
      documentRoot: www/
      envVariables:
        NETTE_DEBUG: "0"
        NETTE_ENV: production
        DATABASE_HOSTNAME: ${db_hostname}
        DATABASE_PORT: ${db_port}
        DATABASE_USER: ${db_user}
        DATABASE_PASSWORD: ${db_password}
        DATABASE_NAME: ${db_dbName}
        REDIS_URI: tcp://${cache_hostname}:${cache_port}
      initCommands:
        - zsc execOnce ${appVersionId}-migration -- php /var/www/bin/console migrations:migrate --no-interaction --allow-no-migration
        - chown -R zerops:zerops /var/www/var/tmp/
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
    type: php-apache@8.3
    enableSubdomainAccess: true
    envSecrets:
      ADMIN_PASSWORD: <@generateRandomString(<24>)>

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: cache
    type: valkey@7.2
    mode: NON_HA
    priority: 10
```

## Contributte-Specific Configuration

Nette DI must be told to inject env vars. In `Bootstrap.php`:
```php
$configurator->addDynamicParameters(['env' => getenv()]);
```
Without this, `%env.DATABASE_HOSTNAME%` references in `.neon` files are undefined.

Redis session config via `contributte/redis`:
```neon
# config/ext/contributte.redis.neon
contributte.redis:
    uri: %env.REDIS_URI%
```

Syslog logging via `contributte/monolog` — required for logs to appear in Zerops log viewer:
```neon
# config/ext/contributte.monolog.neon
monolog:
    handlers:
        syslog:
            class: Monolog\Handler\SyslogHandler
```

## Gotchas

- **`addDynamicParameters` is required** — without it, env vars are not accessible in Nette DI config files via `%env.*%`.
- **Flat DB vars, not DSN** — Contributte uses separate `DATABASE_HOSTNAME`, `DATABASE_PORT`, etc., not a single `DATABASE_DSN`. Wire each individually from `${db_*}` refs.
- **`REDIS_URI: tcp://`** — contributte/redis expects `tcp://hostname:port`, not `redis://`. Use `${cache_hostname}` (var is `hostname`, not `host`).
- **`SyslogHandler` required for Zerops logs** — without it, application logs do not reach the Zerops log collector. `contributte/monolog` package provides the handler.
- **Fixtures are dev-only** — remove the `doctrine:fixtures:load` initCommand for stage/production. Also switch to `--no-dev` composer install for stage.
- **Temp dir is `var/tmp/`** — Contributte convention places temp in `var/tmp/`, not `temp/` (base Nette). The `chown` path must match your actual temp directory.
- **`zsc execOnce` suffix** — migration and fixtures use separate suffixes (`-migration`, `-fixtures`) so they are tracked independently per deploy version.
