---
id: develop-env-var-channels
priority: 2
phases: [develop-active]
title: "Env var channels — when each one goes live"
references-fields: [tools.envChangeResult.RestartedServices, tools.envChangeResult.RestartWarnings, tools.envChangeResult.RestartSkipped, tools.envChangeResult.RestartedProcesses, tools.envChangeResult.Stored]
---

### Env var channels

Two channels set env vars, and the channel determines when the value
goes live.

| Channel | Set with | When live |
|---|---|---|
| Service-level env | `zerops_env action="set"` | Response's `restartedServices` lists hostnames whose runtime containers were cycled; `restartedProcesses` has platform Process details. |
| `run.envVariables` | Edit `zerops.yaml`, commit, deploy | Full redeploy. `zerops_manage action="reload"` does NOT pick them up. |
| `build.envVariables` | Edit `zerops.yaml`, commit, deploy | Next build uses them; not visible at runtime. |

**Suppress restart**: pass `skipRestart=true` — response reports
`restartSkipped: true`; `nextActions` tells you to restart manually
(the value is **not live** until that restart). Partial failures
land in `restartWarnings`; `stored` confirms which keys landed.

**Shadow-loop pitfall**: `zerops_env`-set service-level vars shadow
the same key in `run.envVariables`. Fixing only `zerops.yaml`
won't change the live value — delete the service-level key
(`zerops_env action="delete"`) before redeploy.
