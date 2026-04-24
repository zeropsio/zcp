---
id: develop-dev-server-reason-codes
priority: 4
phases: [develop-active]
runtimes: [dynamic]
title: "zerops_dev_server reason codes"
references-fields: [ops.DevServerResult.Reason, ops.DevServerResult.Running, ops.DevServerResult.HealthStatus, ops.DevServerResult.LogTail]
---

### `reason` values (DevServerResult)

When `zerops_dev_server` actions fail, the response's `reason` field
classifies the failure so you don't need a follow-up call to
diagnose. Dispatch table:

| `reason` | Meaning | Action |
|---|---|---|
| `spawn_timeout` | The remote shell did not detach; stdio handle still owned by child. | You likely hand-rolled `ssh ... "cmd &"` — re-run through `zerops_dev_server action=start`. |
| `health_probe_connection_refused` | Spawn succeeded but nothing is listening on `port`. | Check that your app binds to `0.0.0.0` (not `localhost`), that `port` matches `run.ports[0].port`, and that your start command actually starts a server. Read `logTail` for crash output. |
| `health_probe_http_<code>` | Server runs but returned `<code>` (e.g. 500, 404). | Do NOT restart — it does not fix bugs. Read `logTail` + response body, edit code, deploy. |
| `post_spawn_exit` | No-probe-mode process died after spawn (port=0/healthPath=""). | `action=logs` for consumption errors; typical for worker crashes. |

Observable always: `running` (bool), `healthStatus` (HTTP status
when `port` set, 0 otherwise), `startMillis` (time from spawn to
healthy), `logTail` (last log lines). Use these to confirm state
without a second tool call.
