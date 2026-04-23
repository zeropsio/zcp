---
id: develop-dev-server-triage
priority: 2
phases: [develop-active]
title: "Dev-server state triage — expectation → check → act"
---

### Dev-server state triage

Before deploying, verifying, or iterating on a runtime service, work the
triage rather than blind-starting a process.

**Step 1 — Determine expectation** from service type + mode:

| Service class | Deployed shape | Dev-server owner |
|---|---|---|
| Implicit-webserver (`php-nginx`, `php-apache`, `nginx`, `static`) | Always live post-deploy | Platform — no manual start |
| Dynamic runtime, mode=`dev` | `zsc noop` idle | **Agent** — starts via tool |
| Dynamic runtime, mode=`simple` / `stage` | Real `run.start` + `healthCheck` | Platform auto-starts |

If the service is implicit-webserver, static, or simple/stage-mode
dynamic, you are done with this triage — the platform owns lifecycle.

**Step 2 — Check current state.** For dev-mode dynamic runtimes:

```
# container env
zerops_dev_server action=status hostname="{hostname}" port={port} healthPath="{path}"

# local env — runs on your machine
Bash command="curl -s -o /dev/null -w '%{http_code}' --max-time 2 http://localhost:{port}{path}"
```

Interpret the result:

- Running (HTTP 2xx/3xx/4xx) → proceed to `zerops_verify`.
- Not listening (connection_refused / exit code 000) → start (step 3).
- HTTP 5xx → server runs but is broken. Read logs + response body.
  Do NOT restart — restart does not fix bugs. Diagnose then edit code.

**Step 3 — Act on the delta.** Start the dev server:

```
# container env
zerops_dev_server action=start hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"

# local env
Bash run_in_background=true command="{start-command}"
```

**After every redeploy the dev process is gone** (new container in
container env; framework's own restart behaviour in local env after a
process kill). Re-run step 2 before `zerops_verify`.
