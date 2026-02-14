# Configure: Configuring Zerops Services

## Overview

Manage environment variables, ports, routing, and service configuration.

## Environment Variables

### View Current Variables

Service-level variables:

```
zerops_env action="get" serviceHostname="api"
```

Project-level variables (shared across all services):

```
zerops_env action="get" project=true
```

### Set Variables

```
zerops_env action="set" serviceHostname="api" variables=["DATABASE_URL=postgresql://db:5432/app", "NODE_ENV=production"]
```

For project-wide variables:

```
zerops_env action="set" project=true variables=["SHARED_SECRET=mysecret"]
```

### Delete Variables

```
zerops_env action="delete" serviceHostname="api" variables=["OLD_VAR"]
```

### Cross-Service References

Reference other services using underscore notation:

```
zerops_env action="set" serviceHostname="api" variables=["DB_HOST=${db_hostname}", "CACHE_HOST=${cache_hostname}"]
```

Important: Always use underscores in references (`${db_hostname}`), not dashes.

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

## Configuration Files

### zerops.yml (Build + Deploy + Run)

Defines the full pipeline per service. Validated server-side during deploy.

### import.yml (Infrastructure)

Defines services to create. Service type validation runs automatically before the API call.

## Tips

- Environment variables are applied asynchronously. Track the process to confirm completion.
- Service-level env vars override project-level vars with the same key.
- Use `zerops_discover` with `includeEnvs=true` to see all env vars for a service.
- Changes to `zerops.yml` take effect on next deploy, not immediately.
