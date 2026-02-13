# PHP Apache/Nginx Variants on Zerops

Demonstrates both Apache and Nginx in same project. Build base differs from run base.

## zerops.yml (build vs run base)
```yaml
zerops:
  - setup: apacheapi
    build:
      base: php@8.3  # Generic PHP (no server)
      buildCommands:
        - composer install --optimize-autoloader --no-dev
    run:
      base: php-apache@8.3  # Apache variant
      deployFiles:
        - index.php
        - .htaccess  # Required for Apache
        - vendor/

  - setup: nginxapi
    build:
      base: php@8.3  # Same generic build
    run:
      base: php-nginx@8.3  # Nginx variant
      deployFiles:
        - index.php
        - vendor/  # No .htaccess for Nginx
```

## Gotchas
- **Build base** is generic php@8.3 (for composer), **run base** includes web server
- **Apache requires .htaccess**, Nginx does not (different deployFiles)
- Both services can coexist in same project (separate load balancing)
- Health check endpoint must be implemented in app
