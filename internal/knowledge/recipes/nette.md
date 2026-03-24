# Nette Framework on Zerops

PHP-Apache runtime with PostgreSQL and Valkey sessions. Doctrine migrations via `zsc execOnce`.

## Keywords
nette, tracy, doctrine, contributte, latte, presenter

## TL;DR
Nette on PHP-Apache (`documentRoot: www/`), PostgreSQL via PDO DSN, Valkey for sessions via `tcp://` URI. Migrate with `zsc execOnce`. Fix temp dir permissions in initCommands.

## zerops.yml

```yaml
zerops:
  - setup: app
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
        APP_ENV: local
        APP_DEBUG: "true"
        DATABASE_DSN: pgsql:host=${db_hostname};port=${db_port};dbname=${db_dbName}
        DATABASE_USER: ${db_user}
        DATABASE_PASSWORD: ${db_password}
        REDIS_URI: tcp://${cache_hostname}:${cache_port}
      initCommands:
        - zsc execOnce ${appVersionId} -- php /var/www/bin/console migrations:continue
        - chown -R zerops:zerops /var/www/temp/
      healthCheck:
        httpGet:
          port: 80
          path: /
```

Stage service zerops.yml additions (on top of dev config):
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
        APP_ENV: production
        APP_DEBUG: "false"
        DATABASE_DSN: pgsql:host=${db_hostname};port=${db_port};dbname=${db_dbName}
        DATABASE_USER: ${db_user}
        DATABASE_PASSWORD: ${db_password}
        REDIS_URI: tcp://${cache_hostname}:${cache_port}
      initCommands:
        - zsc execOnce ${appVersionId} -- php /var/www/bin/console migrations:continue
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

## Gotchas

- **`documentRoot: www/`** — Nette's web root is `www/`, not `public/`. Required in zerops.yml.
- **`DATABASE_DSN` uses PDO syntax** — `pgsql:host=...;port=...;dbname=...`. Not a URL. Wire `${db_hostname}`, `${db_port}`, `${db_dbName}` individually.
- **`REDIS_URI: tcp://hostname:port`** — Nette/contributte Redis client expects `tcp://` scheme, not `redis://`. Use `${cache_hostname}` (not `${cache_host}` — that var does not exist).
- **`chown -R zerops:zerops /var/www/temp/`** — Nette writes compiled templates and cache to `temp/`. The zerops user does not own this directory by default; the chown in initCommands is required.
- **`zsc execOnce ${appVersionId}`** — runs migrations exactly once per deploy version across all containers. Required in multi-container setups.
- **`ADMIN_PASSWORD` as envSecret** — per-service random value is correct here (single app service). Unlike encryption keys, admin passwords don't need to be shared between services.
- **No `.env` file** — env vars come from zerops.yml `envVariables` and import.yml `envSecrets`. A `.env` file in the repo shadows OS env vars.
- **Build vs run base** — build uses `php@8.3` (build tools), run uses `php-apache@8.3` (web server). Different base images on same version.
