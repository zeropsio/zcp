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

Edit code on `/var/www/{hostname}/`. The dev process is already
running (see `develop-dynamic-runtime-start-container` for
first-time start). **Code-only edits never trigger
`zerops_deploy`** — deploy is for `zerops.yaml` changes only
(see "**`zerops.yaml` changes**" below).

**Code-only edit cycle**:
- Dev runners with file-watch (`npm run dev`, `vite`, `nodemon`,
  `air`, `fastapi --reload`) pick up edits **only when configured
  for polling** — SSHFS does not surface inotify events. Set
  `CHOKIDAR_USEPOLLING=1` (vite/webpack), `--poll` (nodemon), or
  the runner's equivalent.
- Otherwise (non-watching runner, polling not configured, OR the
  process died), `zerops_dev_server action=restart hostname="{hostname}"
  command="{start-command}" port={port} healthPath="{path}"`.
  The response carries `running`, `healthStatus`, `startMillis`,
  and on failure a `reason` code (see
  `develop-dev-server-reason-codes`) — read it before issuing
  another call.

**`zerops.yaml` changes** (env vars, ports, run-block fields):
`zerops_deploy` first; container is replaced; on the rebuilt
runtime container use `action=start` (NOT restart). See
`develop-platform-rules-common` for the deploy=new-container
rule.

**Diagnostic**: tail the log ring with
`zerops_dev_server action=logs hostname="{hostname}"
logLines=60`. `reason` classifies the failure (connection
refused, HTTP 5xx, spawn timeout, worker exit) without a
follow-up call.
