---
description: "A Python 3.11 application built with Flask and served by Gunicorn, backed by PostgreSQL on Zerops. Demonstrates vendored dependencies via `pip --target`, idempotent database migrations, and a production-ready WSGI setup — paired with six ready-made environment configurations from AI agent workspaces to highly-available production."
---

# Python Hello World on Zerops


# Python Hello World on Zerops


# Python Hello World on Zerops


# Python Hello World on Zerops


# Python Hello World on Zerops


# Python Hello World on Zerops





## Keywords
python, pip, django, flask, gunicorn, uvicorn, zerops.yml, addToRunPrepare, requirements.txt

## TL;DR
Python runtime with pip/git pre-installed. Use `addToRunPrepare` + `run.prepareCommands` for pip install. Bind `0.0.0.0`.

### Base Image

Includes Python, `pip`, `git`.

### Build Procedure (Canonical Pattern)

1. Set `build.base: python@3.14` (or desired version)
2. `build.addToRunPrepare: [requirements.txt]` -- copies to `/home/zerops/`
3. `run.prepareCommands: [python3 -m pip install --ignore-installed -r /home/zerops/requirements.txt]`
4. `build.buildCommands`: NO pip install needed (build container is separate)
5. `build.deployFiles: [app.py, ...]` or `[.]` for all source files
6. `run.start: gunicorn app:app --bind 0.0.0.0:8000`

### Critical Path

`run.prepareCommands` runs BEFORE deploy files arrive at `/var/www` but AFTER `addToRunPrepare` files are at `/home/zerops/`. Always use `/home/zerops/requirements.txt`, NOT `/var/www/requirements.txt`.

### Binding

uvicorn `--host 0.0.0.0`, gunicorn `--bind 0.0.0.0:8000`

### Resource Requirements

**Dev** (install on container): `minRam: 0.5` — `pip install` moderate peak.
**Stage/Prod**: `minRam: 0.25` — WSGI/ASGI workers are lightweight.

### Common Mistakes

- Referencing `/var/www/requirements.txt` in `run.prepareCommands` -> file not found
- Missing `--bind 0.0.0.0` -> 502 Bad Gateway
- Missing `CSRF_TRUSTED_ORIGINS` for Django -> CSRF validation fails behind proxy

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container -- agent starts `python3 app.py` manually via SSH for iteration)
**Prod deploy**: use `addToRunPrepare` + `prepareCommands` pattern for pip install, `start: gunicorn app:app --bind 0.0.0.0:8000`

## zerops.yml

> Reference implementation — learn the patterns, adapt to your project.

```yaml
zerops:
  # Production setup — install deps to ./vendor, compile nothing,
  # deploy minimal artifact. Flask + Gunicorn serve production traffic.
  - setup: prod
    build:
      base: python@3.11

      buildCommands:
        # Install all dependencies into ./vendor so they travel
        # with the application artifact to the runtime container.
        # --target keeps packages inside the project tree (no
        # system-Python pollution) — matches cache and deployFiles.
        - pip install --target=./vendor -r requirements.txt

      deployFiles:
        - ./src        # Flask application package
        - ./vendor     # All pip-installed packages
        - ./migrate.py # DB migration script (runs in initCommands)

      # Reuse ./vendor across builds — pip skips packages already
      # present, cutting subsequent build times significantly.
      cache:
        - vendor

    # Readiness check: Zerops probes GET / before adding the new
    # container to the project balancer — guarantees zero-downtime
    # deploys only route traffic to fully-started containers.
    deploy:
      readinessCheck:
        httpGet:
          port: 8000
          path: /

    run:
      base: python@3.11

      # Run DB migration once per deploy version. initCommands —
      # not buildCommands — ensures migration and code are deployed
      # atomically. zsc execOnce prevents race conditions when
      # multiple containers start simultaneously.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- python migrate.py

      ports:
        - port: 8000
          httpSupport: true

      envVariables:
        # Point Python's module search to the deployed vendor dir.
        PYTHONPATH: /var/www/vendor
        DB_NAME: db
        # Zerops generates these from the 'db' service hostname:
        # ${hostname_key} syntax references generated env variables.
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      # Gunicorn is the production WSGI server. Two workers handle
      # concurrent requests; Flask's built-in server is single-threaded
      # and not suitable for production traffic.
      start: >-
        /var/www/vendor/bin/gunicorn
        --bind 0.0.0.0:8000
        --workers 2
        src.app:app

  # Development setup — deploy full source for SSH-based iteration.
  # The developer SSHs in, runs the app manually, and edits files
  # in place. Zerops prepares the workspace; the developer drives.
  - setup: dev
    build:
      base: python@3.11

      buildCommands:
        # Same vendoring as prod — ./vendor ships with deployFiles: ./
        # so the developer has all dependencies ready after SSH without
        # running pip manually.
        - pip install --target=./vendor -r requirements.txt

      # Deploy the entire working directory — source, vendor, and
      # zerops.yaml (needed if the developer runs zcli push from SSH).
      deployFiles:
        - ./

      cache:
        - vendor

    run:
      base: python@3.11

      # Migration runs on deploy — database is ready when developer
      # SSHs in, no manual setup needed.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- python migrate.py

      ports:
        - port: 8000
          httpSupport: true

      envVariables:
        PYTHONPATH: /var/www/vendor
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      # zsc noop keeps the container alive without starting the app —
      # the developer starts Flask or Gunicorn manually after SSH.
      # Example: /var/www/vendor/bin/gunicorn --bind 0.0.0.0:8000
      #          --workers 2 src.app:app
      start: zsc noop --silent
```
