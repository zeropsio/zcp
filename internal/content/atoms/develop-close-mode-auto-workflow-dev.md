---
id: develop-close-mode-auto-workflow-dev
priority: 3
phases: [develop-active]
deployStates: [deployed]
modes: [dev]
closeDeployModes: [auto]
environments: [container]
multiService: aggregate
title: "close-mode=auto iteration cycle (dev mode)"
references-fields: [ops.DevServerResult.Reason, ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.LogTail, ops.DevServerResult.StartMillis]
references-atoms: [develop-dev-server-reason-codes, develop-platform-rules-container, develop-platform-rules-common]
---

### Development workflow

Edit code at `/var/www/<hostname>/` for each in-scope dev runtime. **Verify the dev process is up first** — every redeploy drops it, and the deployed-state axis only confirms a deploy landed at some point, not that the dev server is currently live. Run `zerops_dev_server action=status hostname="{hostname}" port={port} healthPath="{path}"` per service; if `running: false`, run `action=start`. **Code-only edits never trigger `zerops_deploy`** — deploy is for `zerops.yaml` changes only (see "**`zerops.yaml` changes**" below).

**Code-only edit cycle**:
- Dev runners with file-watch (`npm run dev`, `vite`, `nodemon`, `air`, `fastapi --reload`) pick up edits **only when configured for polling** — SSHFS does not surface inotify events. Set `CHOKIDAR_USEPOLLING=1` (vite/webpack), `--poll` (nodemon), or the runner's equivalent.
- Otherwise (non-watching runner, polling not configured, OR the process died), restart the dev server per service:

```
{services-list:zerops_dev_server action=restart hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"}
```

  The response carries `running`, `healthStatus`, `startMillis`, and on failure a `reason` code — read it before issuing another call.

**`zerops.yaml` changes** (env vars, ports, run-block fields): `zerops_deploy` first; the deploy replaces the runtime container, so on the rebuilt container use `action=start` (NOT restart) — every redeploy needs a fresh dev-process start.

**Diagnostic**: tail the log ring per service:

```
{services-list:zerops_dev_server action=logs hostname="{hostname}" logLines=60}
```

`reason` classifies the failure (connection refused, HTTP 5xx, spawn timeout, worker exit) without a follow-up call.
