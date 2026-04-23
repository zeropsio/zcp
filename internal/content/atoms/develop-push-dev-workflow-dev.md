---
id: develop-push-dev-workflow-dev
priority: 3
phases: [develop-active]
deployStates: [deployed]
modes: [dev]
strategies: [push-dev]
environments: [container]
title: "Push-dev iteration cycle (dev mode, container)"
---

### Development workflow

Edit code on `/var/www/{hostname}/`. Iteration cycle:

1. Edit files on the mount — changes appear instantly inside the container.
2. Restart the dev server so it picks up the edits:

   ```
   zerops_dev_server action=restart hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"
   ```

   `action=restart` is stop + start composed. Port-free polling handles
   SO_REUSEADDR linger; the spawn + probe budgets keep the call bounded
   so a broken start costs seconds, not minutes.
3. Probe the health endpoint from inside the container via the tool:

   ```
   zerops_dev_server action=status hostname="{hostname}" port={port} healthPath="{path}"
   ```

4. Repeat until the change works.

**Code-only changes:** `zerops_dev_server action=restart` — no redeploy required.

**`zerops.yaml` changes** (env vars, ports, entries): redeploy required.
The redeploy kills the container, so the previous dev process is gone —
call `zerops_dev_server action=start` (not `restart`) on the new
container after `zerops_deploy` returns.

**If something goes wrong during iteration**, tail the log ring:

```
zerops_dev_server action=logs hostname="{hostname}" logLines=60
```

Check `reason` on any failed start/restart — codes like
`health_probe_connection_refused`, `health_probe_http_500`,
`spawn_timeout` dispatch to different diagnostic paths.
