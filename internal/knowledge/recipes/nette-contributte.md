# Nette with Contributte on Zerops
Nette framework with Contributte packages for Redis sessions, Monolog syslog logging, and Doctrine migrations.

## Keywords
nette, contributte, php, postgresql, valkey, redis, doctrine, apache, monolog

## TL;DR
Nette + Contributte with Redis sessions and Monolog SyslogHandler -- Doctrine migrations via `zsc execOnce`.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: php@8.3
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
      documentRoot: www/
      envVariables:
        DATABASE_HOSTNAME: ${db_hostname}
        DATABASE_USER: ${db_user}
        DATABASE_PASSWORD: ${db_password}
        DATABASE_NAME: ${db_dbName}
        DATABASE_PORT: ${db_port}
        REDIS_URI: tcp://${redis_hostname}:${redis_port}
      initCommands:
        - zsc execOnce ${appVersionId}-migration -- php /var/www/bin/console migrations:migrate --no-interaction --allow-no-migration
        - zsc execOnce ${appVersionId}-fixtures -- php /var/www/bin/console doctrine:fixtures:load --no-interaction
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
      NETTE_DEBUG: 1
      NETTE_ENV: dev

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
- **DATABASE_HOSTNAME / DATABASE_USER / DATABASE_PASSWORD / DATABASE_NAME / DATABASE_PORT** -- explicit cross-service refs for granular DB config
- **REDIS_URI** -- uses `tcp://${redis_hostname}:${redis_port}` for Redis connection
- **ADMIN_PASSWORD** -- generated via `<@generateRandomString(<24>)>` in import.yml envSecrets
- **NETTE_DEBUG** -- set to `1` for dev; set to `0` for production
- **NETTE_ENV** -- set to `dev` for development; set to `prod` for production
- **documentRoot: www/** -- Nette-specific web root
- **No --no-dev flag** on composer install -- dev dependencies are needed for fixtures in dev mode
- Nette config for Redis sessions:
  ```neon
  # env/base.neon
  contributte.redis:
      uri: %env.REDIS_URI%
  ```
- Nette config for syslog logging:
  ```neon
  # ext/contributte.neon
  monolog:
      handlers:
          syslog:
              class: Monolog\Handler\SyslogHandler
  ```
- Bootstrap.php must inject env vars:
  ```php
  $configurator->addDynamicParameters(['env' => getenv()]);
  ```

## Common Failures
- **Permission denied on var/tmp/** -- `chown -R zerops:zerops /var/www/var/tmp/` in initCommands fixes this
- **Redis connection error** -- verify `REDIS_URI` format is `tcp://hostname:port`
- **Fixtures fail** -- `zsc execOnce ${appVersionId}-fixtures` runs once per deploy; remove for production
- **Logs not appearing in Zerops** -- ensure `contributte/monolog` with `SyslogHandler` is configured

## Gotchas
- **contributte/redis** required for Redis session storage
- **contributte/monolog** with `SyslogHandler` required for Zerops log integration
- **Dynamic env parameters** -- `$configurator->addDynamicParameters(['env' => getenv()])` in Bootstrap.php injects all env vars into Nette DI
- **Admin login**: admin@admin.cz, password from ADMIN_PASSWORD env var
- **Dev mode loads fixtures** -- remove fixtures initCommand and add `--no-dev` to composer install for production
- **zsc execOnce ${appVersionId}-migration** and `${appVersionId}-fixtures` run once per deploy version
- 3 services: app (PHP-Apache) + db (PostgreSQL) + redis (Valkey)
