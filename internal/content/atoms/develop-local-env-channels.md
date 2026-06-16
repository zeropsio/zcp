---
id: develop-local-env-channels
priority: 2
phases: [develop-active]
environments: [local]
title: "Local env channels — three sources, one .env"
coverageExempt: "local-mode three-channel model — covered by env-handling spec + Theme 2 design pass; canonical eval scenarios are container-focused"
---

### Three input channels, one rendered `.env`

Local mode ZCP merges env state from three places into the `.env` file your app reads:

| Channel | Where it lives | Writer | Use for |
|---|---|---|---|
| `project.envVariables` | Zerops project state | `zerops_env action=set scope=project` | Shared secrets that match locally and deployed (APP_KEY, JWT_SECRET, third-party tokens) |
| `zerops.yaml run.envVariables` | git repo, per-service | edit the file | Deployed-only flags (APP_ENV=production), managed-service refs (DATABASE_URL=${db_connectionString}) |
| `.env.local` | CWD, gitignored | YOU edit it | Per-developer overrides (APP_ENV=local, LOG_LEVEL=debug, override DATABASE_URL to local Postgres) |

**`.env` is fully derived.** Re-running `zerops_env action=generate-dotenv` reproduces it deterministically.

<!-- axis-k-keep: signal-#1 — anti-pattern callout: editing .env directly is the failure mode the safety gate prevents -->
Don't edit `.env` directly — next regen refuses with a diff and asks you to move keys to `.env.local` or pass `force=true`.

**`.env.local` is yours.** ZCP never writes it — you create and own it; ZCP only reads it as an overlay merged into `.env`. Add anything you want sticky there.

### Lifecycle — where to put a new env var, then regen

| Need | Action | Then |
|---|---|---|
| Shared secret (same value local + deployed) | `zerops_env action=set scope=project key=X value=Y` | `generate-dotenv` |
| Derived from a managed service (`${db_*}`) | edit `zerops.yaml run.envVariables` | `generate-dotenv` |
| Deployed-only flag (NODE_ENV=production) | edit `zerops.yaml run.envVariables` | next `zerops_deploy` |
| Per-developer override | edit `.env.local` | `generate-dotenv` |
| Add managed service (e.g. redis) | `zerops_import` extension + edit `zerops.yaml` `${redis_*}` ref | `generate-dotenv` |
| Rotate secret | `zerops_env action=set scope=project` | `generate-dotenv`; restart consumers |
| Release a `.env.local` override | delete the line from `.env.local` | `generate-dotenv` |
