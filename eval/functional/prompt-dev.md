# Functional Eval: Dev Deploy

You are testing the ZCP knowledge base by building and deploying a real working application on Zerops. This is NOT a dry run — you will create services, write code, deploy, and verify functionality end-to-end.

## Goal

Deploy a **Bun + PostgreSQL** application in simple mode (single service, no dev+stage).

The app must serve:
- `GET /health` — HTTP 200, confirms the app is running
- `GET /status` — queries PostgreSQL, returns connectivity info
- `GET /` — returns a JSON welcome message

## Constraints

- **Simple mode**: Single `evalapp` runtime + single `evaldb` database. No dev+stage pairs.
- **Hostnames**: Use exactly `evalapp` for the runtime and `evaldb` for the database.
- **Work in `/tmp/evalapp/`**: Create the app source code in this directory.
- **No user interaction**: Do not use AskUserQuestion or TodoWrite. Make all decisions autonomously.

## How to proceed

1. Call `zerops_workflow workflow="bootstrap"` — follow the workflow steps.
2. Call `zerops_knowledge runtime="bun@1.2" services=["postgresql@16"]` — use this to generate correct YAML and app code.
3. Build, deploy, and verify the application using the workflow guidance.

The knowledge base and workflows contain everything you need: import.yml structure, zerops.yml format, env var wiring, deploy process, and verification protocol.

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
