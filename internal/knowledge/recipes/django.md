# Django on Zerops

Python runtime with Gunicorn WSGI server. Start with just the app, add database and storage as needed.

## Keywords
django, gunicorn, wsgi, asgi, celery, django-storages

## TL;DR
Python runtime, Gunicorn on port 8000. SECRET_KEY must be project-level. Wire services with `${hostname_varName}` refs. `addToRunPrepare` + `prepareCommands` installs packages in the run container (build and run are separate). CSRF_TRUSTED_ORIGINS required for Zerops reverse proxy.

## SECRET_KEY

Must be **project-level** (shared across dev+stage). Django uses it for signing sessions, CSRF tokens, and password reset links — dev and stage must share the same value or sessions break across environments.

**Generate**: `python -c "from django.core.management.utils import get_random_secret_key; print(get_random_secret_key())"`
**Set**: `zerops_env project=true variables=["SECRET_KEY=<generated value>"]`

Do NOT use `envSecrets` in import.yml — generates a different key per service (dev and stage get different keys, breaking cross-environment sessions and tokens).

`DJANGO_SUPERUSER_PASSWORD` is OK as per-service `envSecrets` — it's a one-time setup credential, not a shared signing key.

## Wiring Managed Services

Cross-service pattern: `${hostname_varName}` — resolved at container start from the target service's env vars.

After adding any service: `zerops_discover includeEnvs=true` to see available vars. Map ONLY discovered vars — guessing names causes silent failures (unresolved refs stay as literal strings).

### PostgreSQL env vars

| Var | Value |
|-----|-------|
| `${db_hostname}` | host |
| `${db_port}` | 5432 |
| `${db_user}` | user |
| `${db_password}` | password |
| `${db_dbName}` | database name |

### Object Storage env vars

| Var | Value |
|-----|-------|
| `${storage_accessKeyId}` | S3 access key |
| `${storage_secretAccessKey}` | S3 secret key |
| `${storage_bucketName}` | bucket name |
| `${storage_apiUrl}` | S3 endpoint URL |

## Stack Layers

### Layer 0: Just Django (no managed services)

Stateless app — APIs, docs. No persistent data.

**import.yml:**
```yaml
services:
  - hostname: appdev
    type: python@3.12
    startWithoutCode: true
    maxContainers: 1
    enableSubdomainAccess: true
```

**zerops.yml:**
```yaml
zerops:
  - setup: appdev
    build:
      base: python@3.12
      os: alpine
      buildCommands:
        - pip install --no-cache-dir -r requirements.txt
      deployFiles: ./
      addToRunPrepare:
        - requirements.txt
    run:
      base: python@3.12
      os: alpine
      prepareCommands:
        - pip install --no-cache-dir -r requirements.txt
      ports:
        - port: 8000
          httpSupport: true
      envVariables:
        PYTHONDONTWRITEBYTECODE: "1"
        PYTHONUNBUFFERED: "1"
        DJANGO_SETTINGS_MODULE: myproject.settings
        APP_URL: ${zeropsSubdomain}
        DEBUG: "True"
        LOG_LEVEL: DEBUG
      start: gunicorn --bind 0.0.0.0:8000 myproject.wsgi
```

`addToRunPrepare` copies requirements.txt into the run container; `prepareCommands` installs it there. Build and run are separate containers — packages installed at build time are NOT available at runtime unless re-installed via `prepareCommands`.

### Layer 1: + Database (PostgreSQL)

**Add to import.yml:**
```yaml
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

**Add/change in zerops.yml envVariables:**
```yaml
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_NAME: ${db_dbName}
        DB_USER: ${db_user}
        DB_PASSWORD: ${db_password}
```

**Add initCommands:**
```yaml
      initCommands:
        - zsc execOnce migrate-${appVersionId} -- python manage.py migrate
```

**settings.py** (Zerops-specific parts only):
```python
DATABASES = {
    "default": {
        "ENGINE": "django.db.backends.postgresql",
        "HOST": os.environ["DB_HOST"],
        "PORT": os.environ.get("DB_PORT", "5432"),
        "NAME": os.environ["DB_NAME"],
        "USER": os.environ["DB_USER"],
        "PASSWORD": os.environ["DB_PASSWORD"],
    }
}
```

### Layer 2: + File Storage (Object Storage)

**Add to import.yml:**
```yaml
  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10
```

**Add to zerops.yml envVariables:**
```yaml
        USE_S3: "1"
        S3_ACCESS_KEY_ID: ${storage_accessKeyId}
        S3_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        S3_BUCKET_NAME: ${storage_bucketName}
        S3_ENDPOINT_URL: ${storage_apiUrl}
```

**Requires**: `django-storages` and `boto3` in requirements.txt.

**settings.py** S3 config (Zerops-specific — `AWS_S3_ENDPOINT_URL` and path-style required for Zerops MinIO backend):
```python
if os.environ.get("USE_S3") == "1":
    STORAGES = {
        "default": {"BACKEND": "storages.backends.s3boto3.S3Boto3Storage"},
        "staticfiles": {"BACKEND": "storages.backends.s3boto3.S3StaticStorage"},
    }
    AWS_ACCESS_KEY_ID = os.environ["S3_ACCESS_KEY_ID"]
    AWS_SECRET_ACCESS_KEY = os.environ["S3_SECRET_ACCESS_KEY"]
    AWS_STORAGE_BUCKET_NAME = os.environ["S3_BUCKET_NAME"]
    AWS_S3_ENDPOINT_URL = os.environ["S3_ENDPOINT_URL"]
    AWS_S3_ADDRESSING_STYLE = "path"   # required for Zerops MinIO
    AWS_DEFAULT_ACL = None
```

**Add to initCommands:**
```yaml
        - zsc execOnce collectstatic-${appVersionId} -- python manage.py collectstatic --no-input
```

## Dev vs Stage zerops.yml

Managed services are **shared** — both dev and stage use the same `db`, `storage`.

| | Dev | Stage |
|---|-----|-------|
| `DEBUG` | `"True"` | `"False"` |
| `LOG_LEVEL` | `DEBUG` | `INFO` |
| `initCommands` | migrate only | migrate + collectstatic |
| `healthCheck` | omit | port 8000 `/` |
| `readinessCheck` | omit | port 8000 `/` |
| Service refs | `${db_hostname}`, ... | **same** |

Stage zerops.yml (complete entry):
```yaml
zerops:
  - setup: appstage
    build:
      base: python@3.12
      os: alpine
      buildCommands:
        - pip install --no-cache-dir -r requirements.txt
      deployFiles: ./
      addToRunPrepare:
        - requirements.txt
    deploy:
      readinessCheck:
        httpGet:
          port: 8000
          path: /
    run:
      base: python@3.12
      os: alpine
      prepareCommands:
        - pip install --no-cache-dir -r requirements.txt
      ports:
        - port: 8000
          httpSupport: true
      envVariables:
        PYTHONDONTWRITEBYTECODE: "1"
        PYTHONUNBUFFERED: "1"
        DJANGO_SETTINGS_MODULE: myproject.settings
        APP_URL: ${zeropsSubdomain}
        DEBUG: "False"
        LOG_LEVEL: INFO
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_NAME: ${db_dbName}
        DB_USER: ${db_user}
        DB_PASSWORD: ${db_password}
      initCommands:
        - zsc execOnce migrate-${appVersionId} -- python manage.py migrate
        - zsc execOnce collectstatic-${appVersionId} -- python manage.py collectstatic --no-input
      start: gunicorn --bind 0.0.0.0:8000 myproject.wsgi
      healthCheck:
        httpGet:
          port: 8000
          path: /
```

## Proxy and CSRF Configuration

Required in settings.py for all deployments on Zerops:

```python
# Zerops L7 balancer — required for POST requests and correct HTTPS detection
CSRF_TRUSTED_ORIGINS = [os.environ.get("APP_URL", "")]
USE_X_FORWARDED_HOST = True
SECURE_PROXY_SSL_HEADER = ("HTTP_X_FORWARDED_PROTO", "https")
ALLOWED_HOSTS = ["*"]   # or restrict to your domain + Zerops subdomain
```

`APP_URL` is set from `${zeropsSubdomain}` which is always a full `https://...` URL — pass it directly to `CSRF_TRUSTED_ORIGINS`.

## Superuser Setup

```yaml
# import.yml — per-service is OK (one-time credential, not a signing key)
    envSecrets:
      DJANGO_SUPERUSER_PASSWORD: <@generateRandomString(<12>)>
```

```yaml
# zerops.yml initCommands
        - zsc execOnce createsuperuser -- python manage.py createsuperuser --no-input --username admin --email admin@example.com || true
```

No `${appVersionId}` on `createsuperuser` — runs once ever (idempotent via `|| true`), not once per deploy.

## Gotchas

- **SECRET_KEY must be project-level** — per-service `envSecrets` generates different keys for dev and stage, breaking sessions and CSRF tokens across environments
- **CSRF_TRUSTED_ORIGINS** — mandatory behind Zerops HTTP balancer. Without it all POST requests return 403
- **Gunicorn must bind to `0.0.0.0:8000`** — default `127.0.0.1` is unreachable from the Zerops load balancer. Use `--bind 0.0.0.0:8000` flag or `bind = "0.0.0.0:8000"` in gunicorn.conf.py
- **`addToRunPrepare` + `prepareCommands`** — build and run are separate containers. Packages installed at build time do NOT carry over. Copy requirements.txt with `addToRunPrepare`, re-install with `prepareCommands`
- **`AWS_S3_ADDRESSING_STYLE = "path"`** — required for Zerops object storage (MinIO backend uses path-style, not virtual-hosted)
- **No SQLite** — container filesystem is replaced on deploy. Always use a database service
- **`zsc execOnce` handles migration concurrency** — do not add Django's `--skip-checks` or other lock flags; double-locking may require infrastructure (lock table) that doesn't exist yet at init time
- **Replace `myproject`** in WSGI/ASGI module paths with your actual Django project name
