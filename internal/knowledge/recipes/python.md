# Python (Flask) on Zerops

Flask API with PostgreSQL via psycopg2. Minimal Python backend template with runtime dependency install.

## Keywords
python, flask, postgresql, psycopg2, api, wsgi, waitress

## TL;DR
Flask API on port 8000 with PostgreSQL -- dependencies installed at runtime via `prepareCommands`, minimal deploy of `app.py` only.

## zerops.yml
```yaml
zerops:
  - setup: api
    build:
      base: python@3.12
      deployFiles: ./app.py
      addToRunPrepare:
        - requirements.txt
    run:
      base: python@3.12
      prepareCommands:
        - python3 -m pip install --ignore-installed -r requirements.txt
      ports:
        - port: 8000
          httpSupport: true
      envVariables:
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
      start: python3 app.py
      healthCheck:
        httpGet:
          port: 8000
          path: /status
```

## import.yml
```yaml
services:
  - hostname: api
    type: python@3.12
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Configuration

Database connection reads env vars:

```python
import os
import psycopg2

db_host = os.getenv('DB_HOST', 'localhost')
db_port = os.getenv('DB_PORT', '5432')
db_name = os.getenv('DB_NAME', 'db')
db_user = os.getenv('DB_USER', 'db')
db_pass = os.getenv('DB_PASS', '')

conn = psycopg2.connect(
    host=db_host, port=db_port,
    dbname=db_name, user=db_user, password=db_pass
)
```

Health check endpoint:

```python
@app.route('/status')
def status_check():
    return jsonify(status="UP")
```

## Gotchas

- **deployFiles is for stage/production** — this recipe shows the optimized deploy pattern for cross-deploy targets or git-based builds. For self-deploying services (dev or simple mode), use `deployFiles: [.]` so source + zerops.yml survive the deploy. With `[.]`, build output stays in its original directory under `/var/www/` — adjust `start` path accordingly (see Deploy Semantics in platform reference).
- **Bind to 0.0.0.0** -- `app.run(host='0.0.0.0', port=8000)` is required; binding to `127.0.0.1` makes the app unreachable through the Zerops L7 balancer
- **Dependencies installed at runtime** via `prepareCommands`, not during build -- `addToRunPrepare` copies `requirements.txt` to the run container
- **`--ignore-installed` flag** in pip install prevents conflicts with system packages in the Zerops base image
- **Minimal deploy** -- only `app.py` is deployed; for larger projects list all needed files/directories in `deployFiles`
- **${db_hostname}** and other `${db_*}` vars are auto-injected by Zerops from the `db` service
- **Production WSGI server** -- for production workloads, use Gunicorn or Waitress instead of Flask's dev server (e.g., `waitress-serve --host=0.0.0.0 --port=8000 app:app`)
- **No build step** -- this recipe has no `buildCommands` since Python is interpreted; the build phase only stages deploy files
- **healthCheck is for stage/production only** -- the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
