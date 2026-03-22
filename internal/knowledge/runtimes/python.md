# Python on Zerops

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

### Common Mistakes

- Referencing `/var/www/requirements.txt` in `run.prepareCommands` -> file not found
- Missing `--bind 0.0.0.0` -> 502 Bad Gateway
- Missing `CSRF_TRUSTED_ORIGINS` for Django -> CSRF validation fails behind proxy

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container -- agent starts `python3 app.py` manually via SSH for iteration)
**Prod deploy**: use `addToRunPrepare` + `prepareCommands` pattern for pip install, `start: gunicorn app:app --bind 0.0.0.0:8000`
