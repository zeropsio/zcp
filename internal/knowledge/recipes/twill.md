# Twill CMS on Zerops

Laravel Twill CMS. Multi-base build, OS mismatch (Alpine build → Ubuntu run), custom nginx, zsc execOnce.

## zerops.yml (multi-base + OS mismatch)
```yaml
zerops:
  - setup: app
    build:
      base: [php@8.3, nodejs@18]  # Multi-base for assets
      os: alpine
      buildCommands:
        - composer install --optimize-autoloader --no-dev
        - npm install
        - npm run build
    run:
      base: php-nginx@8.3
      os: ubuntu  # Runtime OS differs from build
      siteConfigPath: site.conf.tmpl  # Custom nginx config
      envVariables:
        SESSION_DRIVER: redis
        CACHE_STORE: redis
        FILESYSTEM_DISK: s3
        AWS_USE_PATH_STYLE_ENDPOINT: true  # REQUIRED for Zerops S3
        GLIDE_USE_SOURCE_DISK: s3
      initCommands:
        - sudo -E -u zerops -- zsc execOnce initialize -- php artisan twill:install -n
        - sudo -E -u zerops -- zsc execOnce initializeadmin -- php artisan twill:superadmin twill@zerops.io zerops
        - sudo -E -u zerops -- zsc execOnce ${appVersionId} -- php artisan migrate --isolated --force
        - sudo -E -u zerops -- zsc execOnce initializeSeed -- php artisan db:seed --force
      readinessCheck:
        httpGet:
          port: 80
          path: /up  # Laravel 11 health check
```

## Gotchas
- **Multi-base [php, nodejs]** for frontend assets
- **OS mismatch**: Alpine build (faster) → Ubuntu runtime (compatibility)
- **zsc execOnce** ensures commands run exactly once per deploy
- **AWS_USE_PATH_STYLE_ENDPOINT: true** REQUIRED for Zerops S3
- **league/flysystem-aws-s3-v3** required in composer.json
- Glide uses S3 source disk with local caching
- Custom nginx config via siteConfigPath
