---
id: develop-close-push-dev-dev
priority: 7
phases: [develop-active]
deployStates: [deployed]
modes: [dev]
strategies: [push-dev]
environments: [container]
title: "Close task — push-dev dev mode (no stage)"
references-fields: [ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.StartMillis, ops.DevServerResult.Reason, ops.DevServerResult.LogTail]
---

### Closing the task

Dev mode has no stage pair: deploy the single runtime container,
start the dev server, verify.

```
zerops_deploy targetService="{hostname}" setup="dev"
zerops_dev_server action=start hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"
zerops_verify serviceHostname="{hostname}"
```

After each deploy the container is new and the previous dev server
is gone — check `zerops_dev_server action=status` first. If
`running: true`, proceed to verify. Otherwise call `action=start`;
the response carries `healthStatus` (HTTP status from the probe),
`startMillis` (time-to-healthy), `logTail` (last log lines), and on
failure a `reason` code for diagnosis (see
`develop-dev-server-reason-codes`).

For no-HTTP workers (no `port`/`healthPath`), `running` is derived
from the post-spawn liveness check; `healthStatus` stays 0 — use
`action=logs` to confirm consumption.
