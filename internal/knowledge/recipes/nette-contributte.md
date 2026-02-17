# Nette with Contributte on Zerops

Nette framework with Contributte packages for Redis sessions and syslog logging. Doctrine migrations with zsc execOnce.

## Keywords
nette, contributte, php, postgresql, valkey, redis, doctrine, apache

## TL;DR
Nette + Contributte with Redis sessions and Monolog SyslogHandler — Doctrine migrations via `zsc execOnce`.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: php@8.3
      buildCommands:
        - composer install --optimize-autoloader
    run:
      base: php-apache@8.3
      envVariables:
        DATABASE_HOSTNAME: ${db_hostname}
        DATABASE_USER: ${db_user}
        DATABASE_PASSWORD: ${db_password}
        REDIS_URI: 'tcp://${redis_hostname}:${redis_port}'
      initCommands:
        - zsc execOnce ${appVersionId}-migration -- php /var/www/bin/console migrations:migrate --no-interaction
        - zsc execOnce ${appVersionId}-fixtures -- php /var/www/bin/console doctrine:fixtures:load --no-interaction
        - chown -R zerops:zerops /var/www/var/tmp/
```

## import.yml
```yaml
services:
  - hostname: app
    type: php-apache@8.3
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: redis
    type: valkey@7.2
    mode: NON_HA
    priority: 10
```

## Nette config (REQUIRED)
```neon
# env/base.neon
contributte.redis:
    uri: %env.REDIS_URI%

# ext/contributte.neon
monolog:
    handlers:
        syslog:
            class: Monolog\Handler\SyslogHandler
```

## Bootstrap.php
```php
$configurator->addDynamicParameters(['env' => getenv()]);
```

## Gotchas
- **contributte/redis** for Redis session storage
- **contributte/monolog** with SyslogHandler for Zerops log integration
- **Dynamic env parameters** via addDynamicParameters
- Admin login: admin@admin.cz, password in ADMIN_PASSWORD env var
- Dev mode loads fixtures (remove for production)
