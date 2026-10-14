---
id: develop-dev-server-reason-codes
priority: 4
phases: [develop-active]
runtimes: [dynamic]
modes: [dev]
deployStates: [deployed]
title: "zerops_dev_server reason codes"
references-fields: [ops.DevServerResult.Reason, ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.LogTail]
---

### `reason` values (DevServerResult)

When `zerops_dev_server` actions fail, the response's `reason` field
classifies the failure so you don't need a follow-up call to
diagnose. Dispatch table:

| `reason` | Meaning | Action |
|---|---|---|
| `health_probe_connection_refused` | The server started but nothing is listening on `port`. | Check that your app binds to `0.0.0.0` (not `localhost`), that `port` matches `run.ports[0].port`, and that your start command actually starts a server. Read `logTail` for crash output. |
| `health_probe_http_<code>` | Server runs but returned `<code>` (e.g. 500, 404). | Do NOT restart — it does not fix bugs. Read `logTail` + response body, edit code, deploy. |
| `crashed` | The server's process ended on its own; it stays down until started again. | Read `logTail` and the exit in `message`, fix the cause, `action=start`. |
| `port_in_use` | Something outside the dev server holds its port. | Find and stop that process (often a server started by hand outside `zerops_dev_server`), then `action=start`. |
| `stopped` | The server was stopped (or is declared to start only on request). | `action=start` when you need it. |

Observable always: `running` (bool), `healthStatus` (HTTP status
when `port` set, 0 otherwise), `startMillis` (time from start to
healthy), `logTail` (last log lines). Use these to confirm state
without a second tool call.
