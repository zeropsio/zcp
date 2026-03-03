# Django on Zerops

Django with PostgreSQL, S3 object storage, and Gunicorn WSGI server. Requires CSRF_TRUSTED_ORIGINS for reverse proxy.

## Keywords
django, python, postgresql, s3, gunicorn, wsgi, object-storage, storages

## TL;DR
Django with Gunicorn on port 8000, PostgreSQL, and S3 storage -- requires `CSRF_TRUSTED_ORIGINS` and `django-storages` for Zerops reverse proxy and object storage.

## zerops.yml

```yaml
zerops:
  - setup: app
    build:
      base: python@3.12
      os: alpine
      buildCommands:
        - pip install --no-cache-dir -r requirements.txt
      deployFiles: ./
      addToRunPrepare:
        - requirements.txt
      cache: .venv
    deploy:
      readinessCheck:
        httpGet:
          port: 8000
          path: /
    run:
      base: python@3.12
      os: alpine
      prepareCommands:
        - sudo apk add tzdata
        - pip install --no-cache-dir -r requirements.txt
      ports:
        - port: 8000
          httpSupport: true
      envVariables:
        PYTHONDONTWRITEBYTECODE: "1"
        PYTHONUNBUFFERED: "1"
        APP_URL: ${zeropsSubdomain}
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASSWORD: ${db_password}
        USE_S3: "1"
        S3_ACCESS_KEY_ID: ${storage_accessKeyId}
        S3_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        S3_BUCKET_NAME: ${storage_bucketName}
        S3_ENDPOINT_URL: ${storage_apiUrl}
      initCommands:
        - zsc execOnce migrate-${appVersionId} -- python manage.py migrate
        - zsc execOnce collectstatic-${appVersionId} -- python manage.py collectstatic --no-input
        - zsc execOnce createsuperuser -- python manage.py createsuperuser --no-input --username admin --email admin@example.com || true
      start: gunicorn myproject.wsgi
      healthCheck:
        httpGet:
          port: 8000
          path: /
```

## import.yml

```yaml
#yamlPreprocessor=on
services:
  - hostname: app
    type: python@3.12
    enableSubdomainAccess: true
    envSecrets:
      SECRET_KEY: <@generateRandomBytes(<32>) | toString>
      DJANGO_SUPERUSER_PASSWORD: <@generateRandomString(<12>)>

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10
```

## Configuration

Django settings.py must be configured for the Zerops reverse proxy and S3 storage:

```python
# settings.py — CSRF trusted origins for Zerops L7 balancer
import os

# APP_URL is a full HTTPS URL from zeropsSubdomain (e.g. https://app-1df2.prg1.zerops.app)
CSRF_TRUSTED_ORIGINS = [
    os.environ.get('APP_URL', ''),
]
# Add your custom domain when configured:
# CSRF_TRUSTED_ORIGINS.append("https://yourdomain.com")

# Use forwarded headers from Zerops reverse proxy
USE_X_FORWARDED_HOST = True
SECURE_PROXY_SSL_HEADER = ("HTTP_X_FORWARDED_PROTO", "https")
```

S3 storage configuration with django-storages:

```python
# settings.py — S3 object storage
if os.environ.get("USE_S3") == "1":
    DEFAULT_FILE_STORAGE = "storages.backends.s3boto3.S3Boto3Storage"
    AWS_ACCESS_KEY_ID = os.environ.get("S3_ACCESS_KEY_ID")
    AWS_SECRET_ACCESS_KEY = os.environ.get("S3_SECRET_ACCESS_KEY")
    AWS_STORAGE_BUCKET_NAME = os.environ.get("S3_BUCKET_NAME")
    AWS_S3_ENDPOINT_URL = os.environ.get("S3_ENDPOINT_URL")
    AWS_S3_FILE_OVERWRITE = False
    AWS_DEFAULT_ACL = None
```

Database configuration reading env vars:

```python
# settings.py — PostgreSQL database
DATABASES = {
    "default": {
        "ENGINE": "django.db.backends.postgresql_psycopg2",
        "HOST": os.environ.get("DB_HOST", "db"),
        "PORT": os.environ.get("DB_PORT", "5432"),
        "NAME": os.environ.get("DB_NAME", "db"),
        "USER": os.environ.get("DB_USER", "db"),
        "PASSWORD": os.environ.get("DB_PASSWORD", ""),
    }
}
```

## Common Failures

- **403 CSRF verification failed** -- `CSRF_TRUSTED_ORIGINS` missing the Zerops subdomain URL. Add `APP_URL` (already a full HTTPS URL) to the list.
- **Static files 404** -- `collectstatic` did not run. Check initCommands output in logs. The `zsc execOnce` guard ensures it runs once per deploy version.
- **S3 connection refused** -- `storage` service not yet ready when app starts. The `priority: 10` on storage ensures it is created before the app service.
- **502 Bad Gateway** -- Gunicorn not binding to `0.0.0.0`. By default Gunicorn binds to `127.0.0.1:8000`. Configure `gunicorn.conf.py` with `bind = "0.0.0.0:8000"` or use `gunicorn -b 0.0.0.0:8000 myproject.wsgi`.

## Gotchas

- **CSRF_TRUSTED_ORIGINS** is mandatory in settings.py because Django runs behind the Zerops HTTP balancer proxy. Without it, all POST requests return 403.
- **django-storages** and **boto3** packages are required in requirements.txt for S3 integration.
- **Gunicorn binding** must be `0.0.0.0:8000` (not `127.0.0.1`). Configure via `gunicorn.conf.py` or `-b` flag.
- **Migrations** use `zsc execOnce migrate-${appVersionId}` so they run exactly once per deploy, not per container in multi-container setups.
- **SECRET_KEY** is generated as envSecret in import.yml and must be read via `os.environ["SECRET_KEY"]` in settings.py.
- **PYTHONDONTWRITEBYTECODE=1** and **PYTHONUNBUFFERED=1** prevent .pyc file creation and ensure log output is not buffered.
- Replace the `myproject.wsgi` start command with your actual Django project's WSGI module path.
- **Selective deploy** -- for production, consider listing specific directories in `deployFiles` instead of `./` to reduce deploy size.
- **healthCheck is for stage/production only** — the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
