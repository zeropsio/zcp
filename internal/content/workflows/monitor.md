# Monitor: Monitoring Zerops Services

## Overview

Monitor service health, performance, and activity using logs, events, and process tracking.

**If monitoring reveals issues, switch to the debug workflow** (`zerops_workflow workflow="debug"`) for systematic diagnosis. Do not attempt ad-hoc fixes based on partial monitoring data.

## Steps

### 1. Service Overview

Get current status of all services:

```
zerops_discover
```

For detailed info on a specific service:

```
zerops_discover service="api"
```

Key fields to check:
- **status** — RUNNING (healthy), READY_TO_DEPLOY (never deployed), ACTION_FAILED (needs attention)
- **containerCount** — number of active containers (0 = service is down)
- **scaling** — current CPU/RAM/disk ranges and CPU mode (SHARED vs DEDICATED)

### 2. Activity Timeline

View recent activity across the project:

```
zerops_events limit=20
```

Filter by service:

```
zerops_events serviceHostname="api" limit=10
```

Events include: deploys, restarts, scaling, imports, env updates, subdomain changes. Look for FAILED events — they indicate operations that need attention.

### 3. Log Monitoring

Recent errors:

```
zerops_logs serviceHostname="api" severity="error" since="1h"
```

All logs for recent period:

```
zerops_logs serviceHostname="api" since="30m" limit=200
```

Search for patterns:

```
zerops_logs serviceHostname="api" search="timeout" since="24h"
```

### 4. Process Tracking

All mutating tools (import, deploy, manage, env, scale) now poll automatically and return final statuses. Use `zerops_process` only to cancel a running process or check a historical process:

```
zerops_process processId="<process-id>"
```

Process statuses: PENDING, RUNNING, FINISHED, FAILED, CANCELED.

Cancel a stuck process:

```
zerops_process processId="<process-id>" action="cancel"
```

## Log Severity Levels

- **error** — application errors, crashes, unhandled exceptions.
- **warning** — degraded performance, retries, deprecated usage.
- **info** — normal operational messages, request logs.
- **debug** — verbose debugging output (if enabled in app).

## Time Ranges

Supported formats for `since`:
- Minutes: `30m` (1-1440)
- Hours: `1h`, `24h` (1-168)
- Days: `7d` (1-30)
- ISO 8601: `2024-01-15T00:00:00Z`

## Monitoring Patterns

### Post-deploy verification

After deploying, `zerops_deploy` blocks until complete — check the return value for DEPLOYED status. Then verify:

```
zerops_logs serviceHostname="api" severity="error" since="5m"  # No errors
zerops_logs serviceHostname="api" search="listening|started|ready" since="5m"  # Startup confirmed
zerops_discover service="api"                              # RUNNING
zerops_logs serviceHostname="api" severity="error" since="2m"  # No post-startup errors
```

If subdomain is enabled:
```
zerops_subdomain serviceHostname="api" action="enable"     # Activate routing (idempotent), returns subdomainUrls
# bash: curl -sfm 10 "{subdomainUrls[0]}/health"          # HTTP 200
```

### Daily health check

Review errors and events from the past 24 hours:

```
zerops_events limit=50
zerops_logs serviceHostname="api" severity="error" since="24h"
```

### Incident investigation

Correlate events with logs around the incident time:

```
zerops_events serviceHostname="api" limit=20
zerops_logs serviceHostname="api" since="2024-01-15T10:00:00Z" limit=500
```

If the investigation reveals a clear issue, switch to the debug workflow for structured diagnosis:
```
zerops_workflow workflow="debug"
```

## Tips

- Use `zerops_events` first to get the big picture, then drill into `zerops_logs` for details.
- Log search is text-based — use specific error messages or codes for best results.
- All mutating tools poll automatically. Use `zerops_process` only to cancel a stuck process or inspect historical processes.
- A service with status ACTION_FAILED usually needs a redeploy or config fix — use the debug workflow.
