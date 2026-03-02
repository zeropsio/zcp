# PHP Apache/Nginx Variants on Zerops
Demonstrates both Apache and Nginx PHP runtimes in the same project with PostgreSQL.

## Keywords
php, apache, nginx, php-apache, php-nginx, variants, composer, postgresql

## TL;DR
PHP with Apache and Nginx variants in one project -- build base is generic `php@`, run base includes web server.

## zerops.yml
```yaml
zerops:
  - setup: apacheapi
    build:
      base: php@8.3
      buildCommands:
        - composer install --optimize-autoloader --no-dev
      deployFiles:
        - ./index.php
        - ./.htaccess
        - ./vendor
    run:
      base: php-apache@8.3
      envVariables:
        DB_NAME: db
        DB_HOST: db
        DB_PORT: 5432
        DB_USER: db
        DB_PASS: ${db_password}
      healthCheck:
        httpGet:
          port: 80
          path: /status

  - setup: nginxapi
    build:
      base: php@8.3
      buildCommands:
        - composer install --optimize-autoloader --no-dev
      deployFiles:
        - ./index.php
        - ./vendor
    run:
      base: php-nginx@8.3
      envVariables:
        DB_NAME: db
        DB_HOST: db
        DB_PORT: 5432
        DB_USER: db
        DB_PASS: ${db_password}
      healthCheck:
        httpGet:
          port: 80
          path: /status
```

## import.yml
```yaml
services:
  - hostname: apacheapi
    type: php-apache@8.3
    enableSubdomainAccess: true

  - hostname: nginxapi
    type: php-nginx@8.3
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Configuration
- **Build base** is generic `php@8.3` (for composer), **run base** includes web server (`php-apache@` or `php-nginx@`)
- **DB_PASS** uses `${db_password}` cross-service reference for automatic password injection
- **DB_HOST / DB_NAME** set to `db` matching the PostgreSQL service hostname
- **Health check** on `/status` endpoint -- must be implemented in the application code
- Containers are volatile -- use Object Storage for file uploads
- Recommended: add Valkey for sessions and caching in production

## Common Failures
- **Apache 500 errors** -- ensure `.htaccess` is included in deployFiles for Apache variant
- **Health check failing** -- implement a `/status` endpoint in the application returning HTTP 200
- **Missing vendor/** -- `composer install --optimize-autoloader --no-dev` must run in buildCommands

## Gotchas
- **Build base** is generic `php@8.3` (for composer), **run base** includes web server
- **Apache requires .htaccess** in deployFiles, Nginx does not
- Both services can coexist in the same project with separate load balancing
- **Health check endpoint `/status`** must be implemented in the application
- **DB_PASS** uses `${db_password}` -- no manual secret needed for database password
- 3 services: apacheapi + nginxapi + db (PostgreSQL)
- **healthCheck is for stage/production only** -- the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
