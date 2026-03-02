# Nette Framework on Zerops
Nette with PostgreSQL, Redis sessions, Doctrine migrations via zsc execOnce.

## Keywords
nette, php, postgresql, valkey, redis, doctrine, migrations, apache

## TL;DR
Nette with PHP-Apache, PostgreSQL, and Valkey sessions -- Doctrine migrations via `zsc execOnce`.

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
        DATABASE_DSN: pgsql:host=${db_hostname};port=${db_port};dbname=${db_dbName}
        DATABASE_USER: ${db_user}
        DATABASE_PASSWORD: ${db_password}
        REDIS_URI: tcp://${redis_hostname}:${redis_port}
      initCommands:
        - zsc execOnce ${appVersionId} -- php /var/www/bin/console migrations:continue
        - chown -R zerops:zerops /var/www/temp/
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
      APP_ENV: dev

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
- **DATABASE_DSN** -- uses PDO-style DSN with `${db_hostname}`, `${db_port}`, `${db_dbName}` cross-service refs
- **DATABASE_USER / DATABASE_PASSWORD** -- explicit cross-service references `${db_user}` / `${db_password}`
- **REDIS_URI** -- uses `tcp://${redis_hostname}:${redis_port}` for Redis connection
- **ADMIN_PASSWORD** is generated via `<@generateRandomString(<24>)>` in import.yml envSecrets
- **APP_ENV** -- set to `dev` for development; change to `prod` for production
- **documentRoot: www/** -- Nette serves from the `www/` directory

## Common Failures
- **Permission denied on temp/** -- `chown -R zerops:zerops /var/www/temp/` in initCommands fixes this
- **Migration runs multiple times** -- `zsc execOnce ${appVersionId}` ensures migrations run only once per deploy
- **Redis connection refused** -- verify `REDIS_URI` format is `tcp://hostname:port` (not `redis://`)

## Gotchas
- **zsc execOnce ${appVersionId}** ensures migrations run once per deploy across all containers
- **chown** command in initCommands is required to fix temp directory permissions for the zerops user
- **Sessions** stored in Redis (Valkey) -- not file-based
- **documentRoot: www/** -- Nette-specific web root
- 3 services: app (PHP-Apache) + db (PostgreSQL) + redis (Valkey)
- **healthCheck is for stage/production only** -- the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
