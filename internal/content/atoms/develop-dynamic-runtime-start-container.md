---
id: develop-dynamic-runtime-start-container
priority: 3
phases: [develop-active]
runtimes: [dynamic]
environments: [container]
modes: [dev, standard]
closeDeployModes: [auto, manual, unset]
title: "Dynamic runtime — start dev server via zerops_dev_server"
references-fields: [ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.StartMillis, ops.DevServerResult.Reason, ops.DevServerResult.LogTail, ops.DevServerResult.Port, ops.DevServerResult.HealthPath]
references-atoms: [develop-dev-server-reason-codes, develop-platform-rules-common, develop-platform-rules-container]
---

### Dynamic-runtime dev server

Dev-mode dynamic runtimes deploy with `run.start` omitted — the
runtime container idles and no dev process is live until you start
one. The dev server is unsupervised, so the URL 502s after any
container cycle until restarted: a passing verify means "live now",
not "durably shipped". For an always-on service use simple mode.
Action family on `zerops_dev_server`:

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

<!-- axis-k-keep: signal-#1 — anti-pattern callout for ssh-backgrounded dev process -->
Don't hand-roll `ssh {hostname} "cmd &"`: the SSH session ends with
the call and kills the process. Always go through `zerops_dev_server`.
