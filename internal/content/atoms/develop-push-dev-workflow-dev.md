---
id: develop-push-dev-workflow-dev
priority: 3
phases: [develop-active]
deployStates: [deployed]
modes: [dev]
strategies: [push-dev]
environments: [container]
title: "Push-dev iteration cycle (dev mode)"
references-fields: [ops.DevServerResult.Reason, ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.LogTail, ops.DevServerResult.StartMillis]
references-atoms: [develop-dev-server-reason-codes, develop-platform-rules-container, develop-platform-rules-common]
---

### Development workflow

Edit code on `/var/www/{hostname}/`. After each edit, run:

```
zerops_dev_server action=restart hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"
```

The response carries `running`, `healthStatus`, `startMillis`, and
on failure a `reason` code (see `develop-dev-server-reason-codes`).

**Code-only changes**: `action=restart` is enough — no redeploy.

**`zerops.yaml` changes** (env vars, ports, run-block fields): run
`zerops_deploy` first; on the rebuilt runtime container use `action=start`
(not `restart`) — see `develop-platform-rules-common`.

**If iteration goes sideways**, tail the log ring:

```
zerops_dev_server action=logs hostname="{hostname}" logLines=60
```

Read `reason` on any failed start/restart — it classifies the failure
(connection refused, HTTP 5xx, spawn timeout, worker exit) without a
follow-up call.
