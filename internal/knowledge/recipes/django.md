# Django on Zerops

Django with PostgreSQL, S3 storage, Gunicorn WSGI server. Requires CSRF_TRUSTED_ORIGINS config for reverse proxy.

## zerops.yml (key sections)
```yaml
zerops:
  - setup: app
    build:
      base: python@3.12
      addToRunPrepare: [requirements.txt]
    run:
      prepareCommands:
        - python3 -m pip install --ignore-installed -r /home/zerops/requirements.txt
      envVariables:
        USE_S3: "1"
        S3_ACCESS_KEY_ID: $storage_accessKeyId
        S3_SECRET_ACCESS_KEY: $storage_secretAccessKey
        S3_ENDPOINT_URL: $storage_apiUrl
      initCommands:
        - zsc execOnce migrate-${ZEROPS_appVersionId} -- python manage.py migrate
        - zsc execOnce collectstatic-${ZEROPS_appVersionId} -- python manage.py collectstatic --no-input
      start: gunicorn recipe.wsgi
```

## Gotchas
- **CSRF_TRUSTED_ORIGINS** required in settings.py (Django runs behind Zerops HTTP proxy)
- **django-storages** package required for S3 integration
- Migrations use zsc execOnce with appVersionId for idempotency
- 5 services: app + pg + s3 + mailpit + adminer (dev tools for dev only)
