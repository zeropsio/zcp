# Configure: Configuring Zerops Services

## Overview

Manage environment variables, ports, routing, and service configuration.

**Route by operation:**

| What you want to do | Tool | Notes |
|---------------------|------|-------|
| View env vars (service) | `zerops_discover service="{hostname}" includeEnvs=true` | Shows all env vars including cross-refs |
| View env vars (project) | `zerops_discover includeEnvs=true` | Shows project-level vars on all services |
| Set env vars | `zerops_env action="set"` | Async — returns process ID |
| Delete env vars | `zerops_env action="delete"` | Async — returns process ID |
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

### Step 3 — Track Completion

Env var changes are **asynchronous**. The set/delete call returns a process ID. Track it:

```
zerops_process processId="<id from set/delete>"
```

Wait for FINISHED status before relying on the new values. The service containers restart automatically to pick up env var changes — no manual restart needed.

### Step 4 — Verify

After the process completes, confirm the new values:

```
zerops_discover service="api" includeEnvs=true
```

### Delete Variables

```
zerops_env action="delete" serviceHostname="api" variables=["OLD_VAR"]
```

Same async tracking applies — check `zerops_process` for completion.

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

- Environment variable changes are asynchronous — always track the process to confirm completion.
- Service containers restart automatically after env var changes. No manual restart needed.
- Service-level env vars override project-level vars with the same key.
- Cross-service references use underscores: `${service_hostname}`, never dashes.
- Use `zerops_discover` with `includeEnvs=true` to see all env vars — this is the only way to read env vars.
