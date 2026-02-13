# Nette Framework on Zerops

Nette with PostgreSQL, Redis sessions, Doctrine migrations via zsc execOnce.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: php@8.3
      buildCommands:
        - composer install --optimize-autoloader --no-dev
    run:
      base: php-apache@8.3
      documentRoot: www/
      envVariables:
        DATABASE_DSN: 'pgsql:host=${db_hostname};port=${db_port};dbname=${db_dbName}'
        DATABASE_USER: '${db_user}'
        DATABASE_PASSWORD: '${db_password}'
        REDIS_URI: 'tcp://${redis_hostname}:${redis_port}'
      initCommands:
        - zsc execOnce $appVersionId -- php /var/www/bin/console migrations:continue
        - chown -R zerops:zerops /var/www/temp/
```

## import.yml
```yaml
#zeropsPreprocessor=on
services:
  - hostname: app
    envSecrets:
      ADMIN_PASSWORD: <@generateRandomString(<24>)>
  - hostname: db
    priority: 10
  - hostname: redis
    priority: 10
```

## Gotchas
- **zsc execOnce $appVersionId** ensures migrations run once per deploy
- Sessions stored in Redis (not file-based)
- 4 services: app + pg + keydb + adminer
- chown command fixes temp directory permissions
