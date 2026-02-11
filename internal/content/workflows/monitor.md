# Monitor: Monitoring Zerops Services

## Overview

Monitor service health, performance, and activity using logs, events, and process tracking.

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

### 2. Activity Timeline

View recent activity across the project:

```
zerops_events limit=20
```

Filter by service:

```
zerops_events serviceHostname="api" limit=10
```

Events include: deploys, restarts, scaling, imports, env updates, subdomain changes.

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

Check status of async operations:

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

**Post-deploy check**: After deploying, monitor for errors in the first few minutes.

```
zerops_logs serviceHostname="api" severity="error" since="5m"
```

**Daily health check**: Review errors and events from the past 24 hours.

```
zerops_events limit=50
zerops_logs serviceHostname="api" severity="error" since="24h"
```

**Incident investigation**: Correlate events with logs around the incident time.

```
zerops_events serviceHostname="api" limit=20
zerops_logs serviceHostname="api" since="2024-01-15T10:00:00Z" limit=500
```

## Tips

- Use `zerops_events` first to get the big picture, then drill into `zerops_logs` for details.
- Log search is text-based -- use specific error messages or codes for best results.
- Process IDs from operations (deploy, scale, restart) can be tracked with `zerops_process`.
