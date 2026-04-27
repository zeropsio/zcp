---
id: develop-dynamic-runtime-start-container
priority: 3
phases: [develop-active]
runtimes: [dynamic]
environments: [container]
modes: [dev, standard]
title: "Dynamic runtime — start dev server via zerops_dev_server"
references-fields: [ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.StartMillis, ops.DevServerResult.Reason, ops.DevServerResult.LogTail, ops.DevServerResult.Port, ops.DevServerResult.HealthPath]
references-atoms: [develop-dev-server-reason-codes, develop-platform-rules-common, develop-platform-rules-container]
---

### Dynamic-runtime dev server

Dev-mode dynamic-runtime containers start running `zsc noop` after
deploy — no dev process is live until you start one. Action family
on `zerops_dev_server`:

| Action | Use | Args |
|---|---|---|
| `status` | check before `start` (idempotent) — avoids duplicate listener | `hostname port healthPath` |
| `start` | spawn the dev process | `hostname command port healthPath` |
| `restart` | survives-the-deploy config/code change | `hostname command port healthPath` |
| `logs` | tail recent for diagnosis | `hostname logLines=40` |
| `stop` | end of session, free the port | `hostname port` |

Args:
- `command` — exact `run.start` from `zerops.yaml`.
- `port` — `run.ports[0].port`.
- `healthPath` — app-owned (`/api/health`, `/status`) or `/`.

Response carries `running`, `healthStatus`, `reason`, and `logTail`
— read these before making another call.

**After every redeploy, re-run `action=start` before `zerops_verify`** —
the rebuild drops the dev process (see
`develop-platform-rules-common` for the deploy-replaces-container
rule). The hand-roll `ssh {hostname} "cmd &"` anti-pattern is in
`develop-platform-rules-container`. See `develop-dev-server-reason-codes`
for `reason` values.
