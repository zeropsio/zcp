# Debug: Troubleshooting Zerops Services

## Overview

Diagnose and fix issues with Zerops services using logs, events, and service inspection.

## Steps

### 1. Check Service Status

Get an overview of the service and its current state:

```
zerops_discover service="api"
```

Look for: status (RUNNING, STOPPED, ERROR), container count, resource usage.

### 2. Check Recent Events

See what happened recently (deploys, restarts, scaling):

```
zerops_events serviceHostname="api" limit=10
```

### 3. Read Logs

Check for errors in recent logs:

```
zerops_logs serviceHostname="api" severity="error" since="1h"
```

For broader context, include warnings:

```
zerops_logs serviceHostname="api" severity="warning" since="1h"
```

Search for specific error messages:

```
zerops_logs serviceHostname="api" search="connection refused" since="24h"
```

### 4. Check Environment Variables

Verify environment configuration:

```
zerops_env action="get" serviceHostname="api"
```

### 5. Check Processes

If an operation seems stuck, check its process status:

```
zerops_process processId="<process-id>"
```

## Common Issues

### Connection Refused Between Services

- **Cause**: Using `https://` for internal connections or wrong hostname.
- **Fix**: Always use `http://hostname:port` for internal service communication.
  SSL terminates at the L7 balancer, not between services.

### Service Not Starting

- **Cause**: Bad start command, missing dependencies, or port conflict.
- **Fix**: Check `zerops_logs` for startup errors. Verify `start` command in zerops.yml.
  Ensure ports are in range 10-65435.

### Environment Variables Not Available

- **Cause**: Using dashes in cross-references instead of underscores.
- **Fix**: Use `${service_hostname}` (underscores), not `${service-hostname}`.

### Build Failures

- **Cause**: Missing build dependencies or incorrect buildCommands.
- **Fix**: Check build logs via `zerops_logs`. Ensure `prepareCommands` install all system deps.

### Database Connection Issues

- **Cause**: Wrong connection string or service not ready.
- **Fix**: Use the service hostname (e.g., `http://db:5432`). Check that the DB service
  is in RUNNING status via `zerops_discover`.

## Restart as Last Resort

If investigation doesn't reveal the issue, try restarting:

```
zerops_manage action="restart" serviceHostname="api"
```
