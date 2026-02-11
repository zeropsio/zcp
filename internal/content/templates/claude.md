# Zerops Project — CLAUDE.md

This project is deployed on [Zerops](https://zerops.io), a developer-first PaaS.

## Zerops Architecture

- **Internal networking**: Always use `http://` between services, NEVER `https://`.
  SSL terminates at the L7 load balancer.
- **Service communication**: Use service hostnames (e.g., `http://db:5432`, `http://cache:6379`).
  No `localhost` — services run in separate containers.
- **Ports**: Must be in range 10-65435 (0-9 and 65436+ are reserved).

## Configuration Files

- `zerops.yml` — Build, deploy, and run configuration for each service.
- `import.yml` — Infrastructure-as-code for creating services (optional).

## Environment Variables

- Cross-service references use underscores: `${service_hostname}` (not `${service-hostname}`).
- Service-level vars override project-level vars with the same key.

## Key Commands (via MCP)

```
zerops_discover                    # List project services
zerops_discover service="api"      # Detailed service info
zerops_logs serviceHostname="api"  # View service logs
zerops_env action="get" serviceHostname="api"  # View env vars
```

## Build Pipeline

1. `prepareCommands` — cached, runs once (system deps, global tools).
2. `buildCommands` — runs every deploy (compile, test, bundle).
3. `deployFiles` — files copied to runtime container.
4. `initCommands` — runs on every container start (migrations, warmup).
5. `start` — main process command.
