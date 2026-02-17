# Twill CMS on Zerops

Laravel Twill CMS. Multi-base build, OS mismatch (Alpine build → Ubuntu run), custom nginx, zsc execOnce.

## Keywords
twill, laravel, php, cms, postgresql, valkey, redis, s3, nginx

## TL;DR
Twill CMS with multi-base `[php, nodejs]` build — Alpine build, Ubuntu runtime, custom nginx config.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base:
        - php@8.3
        - nodejs@18
      os: alpine
      buildCommands:
        - composer install --optimize-autoloader --no-dev
        - npm install
        - npm run build
    run:
      base: php-nginx@8.3
      os: ubuntu
      siteConfigPath: site.conf.tmpl
      envVariables:
        SESSION_DRIVER: redis
        CACHE_STORE: redis
        FILESYSTEM_DISK: s3
        AWS_USE_PATH_STYLE_ENDPOINT: true
        GLIDE_USE_SOURCE_DISK: s3
      initCommands:
        - sudo -E -u zerops -- zsc execOnce initialize -- php artisan twill:install -n
        - sudo -E -u zerops -- zsc execOnce initializeadmin -- php artisan twill:superadmin twill@zerops.io zerops
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --isolated --force
        - sudo -E -u zerops -- zsc execOnce initializeSeed -- php artisan db:seed --force
      readinessCheck:
        httpGet:
          port: 80
          path: /up
```

## import.yml
```yaml
services:
  - hostname: app
    type: php-nginx@8.3
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: redis
    type: valkey@7.2
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    priority: 10
```

## Gotchas
- **Multi-base [php, nodejs]** for frontend assets
- **OS mismatch**: Alpine build (faster) → Ubuntu runtime (compatibility)
- **zsc execOnce** ensures commands run exactly once per deploy
- **AWS_USE_PATH_STYLE_ENDPOINT: true** REQUIRED for Zerops S3
- **league/flysystem-aws-s3-v3** required in composer.json
- Glide uses S3 source disk with local caching
- Custom nginx config via siteConfigPath
