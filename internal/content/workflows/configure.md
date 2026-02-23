# Configure: Configuring Zerops Services

## Overview

Manage environment variables, ports, routing, and service configuration.

**Route by operation:**

| What you want to do | Tool | Notes |
|---------------------|------|-------|
| View env vars (service) | `zerops_discover service="{hostname}" includeEnvs=true` | Shows all env vars including cross-refs |
| View env vars (project) | `zerops_discover includeEnvs=true` | Shows project-level vars on all services |
| Set env vars | `zerops_env action="set"` | Blocks until complete |
| Delete env vars | `zerops_env action="delete"` | Blocks until complete |
| Enable subdomain | `zerops_subdomain action="enable"` | Idempotent, safe to call repeatedly |
| Disable subdomain | `zerops_subdomain action="disable"` | |

---

## Environment Variables

### Step 1 — View Current Variables

Service-level variables:

```
zerops_discover service="api" includeEnvs=true
```

This returns all env vars for the service, including cross-service references and auto-injected vars from managed services.

Project-level variables (shared across all services):

```
zerops_discover includeEnvs=true
```

This shows all services with their env vars. Project-level vars appear on every service.

### Step 2 — Set Variables

```
zerops_env action="set" serviceHostname="api" variables=["DATABASE_URL=postgresql://db:5432/app", "NODE_ENV=production"]
```

For project-wide variables:

```
zerops_env action="set" project=true variables=["SHARED_SECRET=mysecret"]
```

### Step 3 — Reload Service

`zerops_env` blocks until the process completes (returns FINISHED/FAILED). However, **running containers do not automatically pick up new env vars**. You MUST reload the service:

```
zerops_manage action="reload" serviceHostname="api"
```

Reload is fast (~4s) vs restart (~14s) and sufficient for picking up new environment variables.

### Step 4 — Verify

After the process completes, confirm the new values:

```
zerops_discover service="api" includeEnvs=true
```

### Delete Variables

```
zerops_env action="delete" serviceHostname="api" variables=["OLD_VAR"]
```

`zerops_env` blocks until the delete process completes. Reload the service afterward to apply changes.

### Cross-Service References

Reference other services using underscore notation:

```
zerops_env action="set" serviceHostname="api" variables=["DB_HOST=${db_hostname}", "CACHE_HOST=${cache_hostname}"]
```

Important: Always use underscores in references (`${db_hostname}`), not dashes. Values showing `${...}` in discover output are expected — they resolve at container runtime, not in the API response.

## Ports and Routing

Ports are configured in `zerops.yml` under `run.ports`. See `zerops_knowledge` for port rules and httpSupport configuration.

## Subdomain Access

Enable the built-in `*.zerops.app` subdomain:

```
zerops_subdomain serviceHostname="api" action="enable"
```

Disable when no longer needed:

```
zerops_subdomain serviceHostname="api" action="disable"
```

Note: `enableSubdomainAccess: true` in import.yml pre-configures the subdomain URL but does NOT activate routing. You MUST call `zerops_subdomain action="enable"` after the first deploy to activate it.

## Configuration Files

### zerops.yml (Build + Deploy + Run)

Defines the full pipeline per service. Validated server-side during deploy. Changes take effect on next deploy, not immediately.

### import.yml (Infrastructure)

Defines services to create. Service type validation runs automatically before the API call.

## Tips

- `zerops_env` blocks until the process completes — no manual polling needed.
- After env var changes, reload the service (`zerops_manage action="reload"`) for the app to pick up new values.
- Service-level env vars override project-level vars with the same key.
- Cross-service references use underscores: `${service_hostname}`, never dashes.
- Use `zerops_discover` with `includeEnvs=true` to see all env vars — this is the only way to read env vars.
