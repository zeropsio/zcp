# PHP Apache/Nginx Variants on Zerops

Demonstrates both Apache and Nginx in same project. Build base differs from run base.

## Keywords
php, apache, nginx, php-apache, php-nginx, variants, composer

## TL;DR
PHP with Apache and Nginx variants in one project — build base is generic `php@`, run base includes web server.

## zerops.yml
```yaml
zerops:
  - setup: apacheapi
    build:
      base: php@8.3
      buildCommands:
        - composer install --optimize-autoloader --no-dev
    run:
      base: php-apache@8.3
      deployFiles:
        - index.php
        - .htaccess
        - vendor/

  - setup: nginxapi
    build:
      base: php@8.3
    run:
      base: php-nginx@8.3
      deployFiles:
        - index.php
        - vendor/
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
```

## Gotchas
- **Build base** is generic php@8.3 (for composer), **run base** includes web server
- **Apache requires .htaccess**, Nginx does not (different deployFiles)
- Both services can coexist in same project (separate load balancing)
- Health check endpoint must be implemented in app
