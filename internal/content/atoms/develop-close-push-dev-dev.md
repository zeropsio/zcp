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
references-atoms: [develop-dev-server-reason-codes, develop-dynamic-runtime-start-container, develop-platform-rules-common]
---

### Closing the task

Dev mode has no stage pair: deploy the single runtime container,
start the dev server, verify.

```
zerops_deploy targetService="{hostname}" setup="dev"
zerops_dev_server action=start hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"
zerops_verify serviceHostname="{hostname}"
```

Each deploy gives a new container with no dev server — check
`action=status` first; if `running: false`, call `action=start`.
See `develop-dynamic-runtime-start-container` for parameters and
response shape; `develop-dev-server-reason-codes` for `reason`
triage.

For no-HTTP workers (no `port`/`healthPath`), `running` derives
from the post-spawn liveness check; `healthStatus` stays 0 — use
`action=logs` to confirm consumption.
