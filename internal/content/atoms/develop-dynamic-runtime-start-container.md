---
id: develop-dynamic-runtime-start-container
priority: 3
phases: [develop-active]
runtimes: [dynamic]
environments: [container]
modes: [dev, standard]
title: "Dynamic runtime — start dev server via zerops_dev_server (container)"
---

### Dynamic-runtime dev server (container)

After a deploy to a dev-mode dynamic-runtime service the container runs
`zsc noop` — the dev process is **agent-owned**. Start it via the
`zerops_dev_server` MCP tool. The tool detaches the process correctly
(`ssh -T -n` + `setsid` + stdio redirect), bounds every phase with a
tight budget (spawn 8 s, probe waitSeconds+5 s, tail 5 s), and returns
structured `{running, healthStatus, startMillis, logTail, reason}` so
failures are diagnosable without a follow-up call.

**Start:**

```
zerops_dev_server action=start hostname={hostname} command="{start-command}" port={port} healthPath="{path}"
```

- `command`: the exact shell command from `run.start` in `zerops.yaml`
  (e.g. `npm run start:dev`, `bun run index.ts`, `python app.py`).
- `port`: the HTTP port from `run.ports[0].port`.
- `healthPath`: an app-owned path (`/api/health`, `/status`) if defined;
  else `/`.

**Check if already running (idempotent):**

```
zerops_dev_server action=status hostname={hostname} port={port} healthPath="{path}"
```

Call this BEFORE `action=start` when uncertain — avoids spawning a
second listener on a port already bound.

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

**After every redeploy the container is new — the previous dev process
is gone.** Re-run `action=start` before calling `zerops_verify`. Do NOT
hand-roll `ssh {hostname} "cmd &"` — backgrounded commands hold the
SSH channel open until the 120-second timeout fires because the child
still owns stdio. The tool is the single canonical primitive for
dev-server lifecycle in container env.
