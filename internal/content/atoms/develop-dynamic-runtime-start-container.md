---
id: develop-dynamic-runtime-start-container
priority: 3
phases: [develop-active]
runtimes: [dynamic]
environments: [container]
modes: [dev, standard]
title: "Dynamic runtime — start dev server via zerops_dev_server (container)"
references-fields: [ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.StartMillis, ops.DevServerResult.Reason, ops.DevServerResult.LogTail, ops.DevServerResult.Port, ops.DevServerResult.HealthPath]
references-atoms: [develop-dev-server-reason-codes, develop-platform-rules-common, develop-platform-rules-container]
---

### Dynamic-runtime dev server (container)

Dev-mode dynamic-runtime containers start running `zsc noop` after
deploy — no dev process is live until you start one.

**Start:**

```
zerops_dev_server action=start hostname={hostname} command="{start-command}" port={port} healthPath="{path}"
```

- `command`: the exact shell command from `run.start` in
  `zerops.yaml` (e.g. `npm run start:dev`, `bun run index.ts`,
  `python app.py`).
- `port`: the HTTP port from `run.ports[0].port`.
- `healthPath`: an app-owned path (`/api/health`, `/status`) if
  defined; else `/`.

The response carries `running`, `healthStatus` (HTTP status of the
health probe), `startMillis` (time from spawn to healthy), and on
failure a concrete `reason` code plus `logTail` so you can diagnose
without a follow-up call.

**Check before starting (idempotent status):**

```
zerops_dev_server action=status hostname={hostname} port={port} healthPath="{path}"
```

Call this BEFORE `action=start` when uncertain — avoids spawning a
duplicate listener on a port already bound.

**Restart after config or code change that survived the deploy:**

```
zerops_dev_server action=restart hostname={hostname} command="{start-command}" port={port} healthPath="{path}"
```

**Tail recent logs for diagnosis:**

```
zerops_dev_server action=logs hostname={hostname} logLines=40
```

**Stop at end of session or to free the port:**

```
zerops_dev_server action=stop hostname={hostname} port={port}
```

**After every redeploy, re-run `action=start` before `zerops_verify`** —
the rebuild drops the dev process (see
`develop-platform-rules-common` for the deploy-replaces-container
rule). The hand-roll `ssh {hostname} "cmd &"` anti-pattern is in
`develop-platform-rules-container`. See `develop-dev-server-reason-codes`
for `reason` values.
