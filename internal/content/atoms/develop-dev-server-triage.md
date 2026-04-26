---
id: develop-dev-server-triage
priority: 2
phases: [develop-active]
runtimes: [dynamic]
deployStates: [deployed]
title: "Dev-server state triage — expectation → check → act"
references-fields: [workflow.ServiceSnapshot.RuntimeClass, workflow.ServiceSnapshot.Mode, ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.Reason]
references-atoms: [develop-dev-server-reason-codes, develop-change-drives-deploy]
---

### Dev-server state triage

Before deploying, verifying, or iterating on a runtime service, run
the triage rather than blind-starting a process.

**Step 1 — Determine the expectation** from `runtimeClass` + `mode`
in the envelope:

| Envelope shape | Deployed runtime shape | Dev-server lifecycle |
|---|---|---|
| `runtimeClass: implicit-webserver` | Always live post-deploy | Platform-owned — no manual start |
| `runtimeClass: dynamic`, `mode: dev` | `zsc noop` idle container | You start it via `zerops_dev_server action=start` |
| `runtimeClass: dynamic`, `mode: simple\|stage` | Foreground binary with `healthCheck` | Platform auto-starts and probes |

If the envelope reports implicit-webserver, static, or
simple/stage-mode dynamic, triage ends — platform owns lifecycle.

**Step 2 — Check current state** for dev-mode dynamic:

```
# container env
zerops_dev_server action=status hostname="{hostname}" port={port} healthPath="{path}"

# local env — runs on your machine
Bash command="curl -s -o /dev/null -w '%{http_code}' --max-time 2 http://localhost:{port}{path}"
```

Read the response:

- `running: true` with HTTP 2xx/3xx/4xx `healthStatus` → proceed to
  `zerops_verify`.
- `running: false` with `reason: health_probe_connection_refused` →
  start (step 3).
- `running: true` with `healthStatus: 5xx` → server runs but is
  broken; read logs and response body; do NOT restart (does not
  fix bugs). Edit code, then follow the mode-specific iteration
  cadence (dev-mode: `action=restart`; simple/stage: `zerops_deploy`)
  per `develop-change-drives-deploy`.

For workers with no HTTP surface (`port=0`, `healthPath=""`), skip
HTTP status; call `zerops_logs` to confirm consumption.

**Step 3 — Act on the delta.**

```
# container env
zerops_dev_server action=start hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"

# local env
Bash run_in_background=true command="{start-command}"
```

After every redeploy the dev process is gone — re-run Step 2 before
`zerops_verify`.
