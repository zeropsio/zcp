---
id: develop-close-mode-auto-dev
priority: 7
phases: [develop-active]
deployStates: [deployed]
modes: [dev]
runtimes: [dynamic]
closeDeployModes: [auto]
environments: [container]
multiService: aggregate
title: "Close task — close-mode=auto, dev mode (no stage)"
references-fields: [ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.StartMillis, ops.DevServerResult.Reason, ops.DevServerResult.LogTail]
references-atoms: [develop-dev-server-reason-codes, develop-dynamic-runtime-start-container, develop-platform-rules-common]
---

### Closing the task

Dev mode has no stage pair: deploy the single runtime container, start the dev server, verify. Run for each in-scope dev runtime:

```
{services-list:zerops_deploy targetService="{hostname}" setup="dev"
zerops_dev_server action=start hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"
zerops_verify serviceHostname="{hostname}"}
```

Each redeploy gives a new container with no dev server — check `action=status` first; if `running: false`, call `action=start`. The response carries `running`, `healthStatus`, `startMillis`, and on failure a `reason` code — read it before issuing another call.

For no-HTTP workers (no `port`/`healthPath`), `running` derives from the post-spawn liveness check; `healthStatus` stays 0 — use `action=logs` to confirm consumption.
