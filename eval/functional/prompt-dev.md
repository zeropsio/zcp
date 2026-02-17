# Functional Eval: Dev Deploy

You are testing the ZCP knowledge base by deploying a real working application on Zerops.

## Goal

Deploy a **Bun + PostgreSQL** application on Zerops.

## Constraints

- Hostnames: Use exactly `evalapp` for the runtime and `evaldb` for the database.
- Work in `/tmp/evalapp/` for source code.
- Do not use AskUserQuestion or TodoWrite. Make all decisions autonomously.

## Requirements

The deployed app must:
- Respond on `GET /health` with HTTP 200
- Respond on `GET /status` with a JSON result confirming PostgreSQL connectivity
- Be publicly accessible via subdomain

## Report

After verification, output this exact block with actual values:

```
=== EVAL RESULT ===
scenario: bun-postgresql-dev
import: {PASS|FAIL}
build: {PASS|FAIL}
deploy: {PASS|FAIL}
health_check: {PASS|FAIL}
db_connectivity: {PASS|FAIL}
subdomain_url: {url or N/A}
health_response: {response body}
status_response: {response body}
verdict: {PASS|FAIL}
=== END RESULT ===
```

**Verdict is PASS** only if ALL of: import, build, deploy, health_check, db_connectivity are PASS.
