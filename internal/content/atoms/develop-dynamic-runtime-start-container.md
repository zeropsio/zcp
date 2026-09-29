---
id: develop-dynamic-runtime-start-container
priority: 3
phases: [develop-active]
runtimes: [dynamic]
environments: [container]
modes: [dev, standard]
closeDeployModes: [auto, manual, unset]
title: "Dynamic runtime — start dev server via zerops_dev_server"
references-fields: [ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.StartMillis, ops.DevServerResult.Reason, ops.DevServerResult.LogTail, ops.DevServerResult.Port, ops.DevServerResult.HealthPath, ops.DeployResult.DevServer]
references-atoms: [develop-dev-server-reason-codes]
---

### Dynamic-runtime dev server

Dev-mode dynamic runtime containers start running `zsc noop --silent`
after deploy — a no-op keepalive; no dev process is live until you start
one. Once started, zcp keeps it — one per dev container, the last you
started: when the dev container restarts or is redeployed, zcp starts
it again with the same command, working directory and port — a
deploy's response reports it under `devServer` — until you `stop` it.
A server that crashes in its container stays down for you to read and
fix. It is still a dev process: a passing
verify means "live now", not "durably shipped" — for an always-on
service use simple mode. Action family on `zerops_dev_server`:

| Action | Use | Args |
|---|---|---|
| `status` | check before `start` (idempotent) — avoids duplicate listener | `hostname port healthPath` |
| `start` | spawn the dev process | `hostname command port healthPath` |
| `restart` | survives-the-deploy config/code change | `hostname command port healthPath` |
| `logs` | tail recent for diagnosis | `hostname logLines=40` |
| `stop` | free the port; zcp stops keeping it | `hostname port` |

Args:
- `command` — the app's dev-server start command (the real long-running
  process, e.g. `npm run dev`). NOT the `zsc noop --silent` keepalive that
  sits in the dev block's `run.start`.
- `port` — `run.ports[0].port`.
- `healthPath` — app-owned (`/api/health`, `/status`) or `/`.

Response carries `running`, `healthStatus`, `url` (the hostname-vantage
address to reach the server — the probe runs localhost inside the
container, so the app must bind `0.0.0.0`, not loopback), `reason`, and
`logTail` — read these before making another call.

<!-- axis-k-keep: signal-#1 — anti-pattern callout for ssh-backgrounded dev process -->
Don't hand-roll `ssh {hostname} "cmd &"`: the SSH session ends with
the call and kills the process. Always go through `zerops_dev_server`.
