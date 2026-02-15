# Functional Eval: Dev Deploy

You are testing the ZCP knowledge base by building and deploying a real working application on Zerops. This is NOT a dry run — you will create services, write code, deploy, and verify functionality end-to-end.

## Goal

Deploy a **Bun + PostgreSQL** application in simple mode (single service, no dev+stage). The app must:
1. Connect to PostgreSQL using auto-injected environment variables
2. Serve `/health` — returns HTTP 200 with `{"status":"ok"}`
3. Serve `/status` — queries PostgreSQL (`SELECT 1`) and returns connectivity info
4. Serve `/` — returns a simple JSON welcome message

## Constraints

- **Simple mode**: Single `evalapp` runtime + single `evaldb` database. No dev+stage pairs.
- **Use the knowledge base**: Call `zerops_workflow workflow="bootstrap"` and `zerops_knowledge` to get the correct configuration. Do NOT hardcode values from memory.
- **Hostnames**: Use exactly `evalapp` for the runtime and `evaldb` for the database.
- **No user interaction**: Do not use AskUserQuestion. Make all decisions autonomously.
- **Work in `/tmp/evalapp/`**: Create the app source code in this directory.
- **Report results**: At the end, output a structured result block (see below).

## Steps

### Phase 1: Setup

1. Call `zerops_discover` to check current project state.
2. Call `zerops_knowledge runtime="bun@1.2" services=["postgresql@16"]` to load the correct configuration.
3. Generate `import.yml` for simple mode: `evaldb` (PostgreSQL 16, NON_HA, priority 10) + `evalapp` (Bun 1.2, priority 5, enableSubdomainAccess, startWithoutCode).
4. Wire environment variables: `evalapp` must reference `evaldb` credentials via `${evaldb_...}` syntax.
5. Validate with `zerops_import dryRun=true`, then execute real import.
6. Wait for all services to reach ACTIVE/RUNNING status.

### Phase 2: Application Code

7. Create `/tmp/evalapp/` directory.
8. Write `package.json` with project name and any dependencies needed (e.g., `pg` or `postgres` for PostgreSQL client).
9. Write `src/index.ts` — the Bun application:
   - Bind to `0.0.0.0` on the port from `PORT` env var (default 3000).
   - `/health` — return `{"status":"ok"}` with 200.
   - `/status` — connect to PostgreSQL using env vars (`DATABASE_HOST`, `DATABASE_PORT`, `DATABASE_USER`, `DATABASE_PASSWORD`, `DATABASE_NAME`), run `SELECT 1`, return `{"database":"connected"}` on success or `{"database":"error","detail":"..."}` on failure.
   - `/` — return `{"message":"evalapp running","runtime":"bun"}`.
10. Write `zerops.yml` for the `evalapp` service using knowledge from step 2.

### Phase 3: Deploy

11. Call `zerops_deploy targetService="evalapp" workingDir="/tmp/evalapp"` to trigger the build.
12. Wait 5 seconds, then poll `zerops_events serviceHostname="evalapp" limit=5` every 10 seconds until build reaches terminal status (max 300 seconds / 30 polls).
13. If build FAILED: check logs, fix the issue, redeploy (max 2 retries).

### Phase 4: Verify (7-Point Protocol)

Run all 7 verification checks:

| # | Check | Tool | Pass criteria |
|---|-------|------|---------------|
| 1 | Build completed | zerops_events | Terminal status, not FAILED |
| 2 | Service status | zerops_discover service="evalapp" | RUNNING |
| 3 | No error logs | zerops_logs severity="error" since="5m" | Empty |
| 4 | Startup confirmed | zerops_logs search="listening\|started\|ready" since="5m" | At least one match |
| 5 | No post-startup errors | zerops_logs severity="error" since="2m" | Empty |
| 6 | HTTP health | bash: curl -sfm 10 "{zeropsSubdomain}/health" | HTTP 200 + body contains "ok" |
| 7 | DB connectivity | bash: curl -sfm 10 "{zeropsSubdomain}/status" | HTTP 200 + body contains "connected" |

Get `zeropsSubdomain` from `zerops_discover service="evalapp" includeEnvs=true`. It is already a full URL — do NOT prepend `https://`.

### Phase 5: Report

Output this exact block with actual values filled in:

```
=== EVAL RESULT ===
timestamp: {ISO 8601}
scenario: bun-postgresql-dev
hostnames: evalapp, evaldb
import: {PASS|FAIL}
build: {PASS|FAIL}
deploy: {PASS|FAIL}
health_check: {PASS|FAIL}
db_connectivity: {PASS|FAIL}
subdomain_url: {url or N/A}
health_response: {response body}
status_response: {response body}
total_tool_calls: {count}
errors: {count}
retries: {count}
verdict: {PASS|FAIL}
=== END RESULT ===
```

**Verdict is PASS** only if ALL of: import, build, deploy, health_check, db_connectivity are PASS.

## Troubleshooting

- **Build fails with "command not found"**: Check `zerops.yml` buildCommands — `bun install` must be present.
- **502 Bad Gateway**: App is not binding to `0.0.0.0`. Check the `hostname` parameter in `Bun.serve()`.
- **DB connection refused**: Check env var names match what's in import.yml envVariables. PostgreSQL port is 5432.
- **Env vars show `${...}` literals**: Service needs restart. Call `zerops_manage action="restart" serviceHostname="evalapp"`, wait, re-check.
- **zerops_deploy fails**: Ensure zerops.yml exists at `/tmp/evalapp/zerops.yml` with `setup: evalapp`.
