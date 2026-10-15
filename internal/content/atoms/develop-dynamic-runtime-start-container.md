---
id: develop-dynamic-runtime-start-container
priority: 3
phases: [develop-active]
runtimes: [dynamic]
environments: [container]
modes: [dev, standard]
closeDeployModes: [auto, manual, unset]
multiService: aggregate
title: "Dynamic runtime — start dev server via zerops_dev_server"
references-fields: [ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.StartMillis, ops.DevServerResult.Reason, ops.DevServerResult.LogTail, ops.DevServerResult.Port, ops.DevServerResult.HealthPath, ops.DeployResult.DevServer]
references-atoms: [develop-dev-server-reason-codes]
---

### Dynamic-runtime dev server

Dev-mode dynamic runtime containers run no dev process until you start
one. It runs under the dev service's supervisor (`zcp-devsup`, the dev
setup's `run.start`); its first start declares it in `.zcp/dev-servers.json`.
If the dev setup still runs `zsc noop`, that start adds the supervisor's two
lines and redeploys once (uncommitted work stays) — commit both files. The
supervisor starts it again after a restart or redeploy (a deploy reports it
under `devServer`) until you `stop` it; a crash leaves it down. A passing
verify means "live now", not "durably shipped" — for an always-on service
use simple mode. Action family on `zerops_dev_server`:

| Action | Use | Args |
|---|---|---|
| `status` | check before `start` (idempotent) | `hostname port healthPath` |
| `start` | run the dev process | `hostname command port healthPath` |
| `restart` | after a config/code change | `hostname healthPath` |
| `logs` | tail its log | `hostname logLines=40` |
| `stop` | end its process group, free the port | `hostname` |

Args:
- `command` — the app's dev-server start command (e.g. `npm run dev`),
  not the dev block's `run.start`.
- `port` — `run.ports[0].port`; `healthPath` — app-owned or `/`.
- `name` — when one dev service runs several servers (`web`, `api`);
  stopping one leaves the others running.

Response carries `running`, `healthStatus`, `url` (the hostname-vantage
address to reach the server — the probe runs localhost inside the
container, so the app must bind `0.0.0.0`, not loopback), `reason`, and
`logTail` — read these before making another call.

<!-- axis-k-keep: signal-#1 — anti-pattern callout for ssh-backgrounded dev process -->
Don't hand-roll `ssh {hostname} "cmd &"`: the SSH session ends with
the call and kills the process. Always go through `zerops_dev_server`.
