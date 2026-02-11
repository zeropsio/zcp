# Python on Zerops

## Keywords
python, pip, flask, fastapi, django, uvicorn, gunicorn, virtualenv, requirements.txt, poetry

## TL;DR
Python on Zerops uses Alpine base with pip. Use `addToRunPrepare` under `build:` to copy files from build to run container. No default port.

## Zerops-Specific Behavior
- Versions: 3.12
- Base: Alpine (default)
- Package manager: pip (pre-installed)
- Working directory: `/var/www`
- No default port — must configure
- Use `build.addToRunPrepare` to copy installed packages to runtime

## Configuration
```yaml
zerops:
  - setup: myapp
    build:
      base: python@3.12
      addToRunPrepare:
        - .
      buildCommands:
        - pip install --no-cache-dir -r requirements.txt
      deployFiles: ./
    run:
      start: python -m uvicorn main:app --host 0.0.0.0 --port 8000
      ports:
        - port: 8000
          httpSupport: true
```

## Framework Patterns

### FastAPI
```yaml
zerops:
  - setup: api
    build:
      base: python@3.12
      addToRunPrepare:
        - .
      buildCommands:
        - pip install --no-cache-dir -r requirements.txt
      deployFiles: ./
    run:
      start: python -m uvicorn main:app --host 0.0.0.0 --port 8000
      ports:
        - port: 8000
          httpSupport: true
```

### Django
```yaml
zerops:
  - setup: web
    build:
      base: python@3.12
      addToRunPrepare:
        - .
      buildCommands:
        - pip install --no-cache-dir -r requirements.txt
        - python manage.py collectstatic --noinput
      deployFiles: ./
    run:
      start: gunicorn myproject.wsgi:application --bind 0.0.0.0:8000
      ports:
        - port: 8000
          httpSupport: true
      envVariables:
        DJANGO_SETTINGS_MODULE: myproject.settings
```

### Flask
```yaml
zerops:
  - setup: api
    build:
      base: python@3.12
      addToRunPrepare:
        - .
      buildCommands:
        - pip install --no-cache-dir -r requirements.txt
      deployFiles: ./
    run:
      start: gunicorn app:app --bind 0.0.0.0:5000
      ports:
        - port: 5000
          httpSupport: true
```

## `addToRunPrepare` Pattern

`addToRunPrepare` is listed under `build:` and copies files/directories from the build container into the run container's base image. This is how you persist pip-installed packages in the runtime:

```yaml
build:
  addToRunPrepare:
    - .                          # copies installed packages to run container
  buildCommands:
    - pip install -r requirements.txt
```

For system libraries needed at runtime (e.g., libpq), use `run.prepareCommands`:
```yaml
run:
  prepareCommands:
    - apk add --no-cache libpq
```

## Gotchas
1. **`addToRunPrepare` is under `build:`**: It copies files from build to run — it's not a runtime command
2. **Alpine musl issues**: Some C extension packages (numpy, pandas) may need `prepareCommands` to install build tools
3. **No default port**: Must explicitly bind to `0.0.0.0:PORT` — localhost won't work
4. **System libraries for runtime**: Use `run.prepareCommands` with `apk add` for runtime-only system deps (e.g., libpq)

## See Also
- zerops://services/_common-runtime
- zerops://examples/zerops-yml-runtimes
- zerops://examples/connection-strings
