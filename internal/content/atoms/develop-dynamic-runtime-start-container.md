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

A dev server runs under the dev service's supervisor (`zcp-devsup`, the dev
setup's `run.start`); its first start declares it in `.zcp/dev-servers.json`.
If the dev setup still runs `zsc noop`, that start adds two supervisor
lines, redeploys once (uncommitted work stays), commits both files or
says how.
Every deploy starts each `autostart` server (see `devServer`), a restart
each not `stop`ped; a start crash is retried once, a later one stays down.
A passing verify means "live now", not "durably shipped" — for an
always-on service use simple mode.

- `command` is the app's dev-server command (e.g. `npm run dev`), not the
  dev setup's `run.start`.
- `name` each server when one dev service runs several (`web`, `api`);
  stopping one leaves the others running.
- The health probe runs on localhost inside the dev container, so the app
  must bind `0.0.0.0`, not loopback, for the response's `url` to reach it. Read
  `running`, `healthStatus`, `reason` and `logTail` before another call.

<!-- axis-k-keep: signal-#1 — anti-pattern callout for ssh-backgrounded dev process -->
Don't hand-roll `ssh {hostname} "cmd &"`: the SSH session ends with
the call and kills the process.
