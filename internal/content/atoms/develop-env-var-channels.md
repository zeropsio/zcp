---
id: develop-env-var-channels
priority: 2
phases: [develop-active]
title: "Env var channels"
references-fields: [tools.envChangeResult.RestartedServices, tools.envChangeResult.RestartWarnings, tools.envChangeResult.RestartSkipped, tools.envChangeResult.RestartedProcesses, tools.envChangeResult.Stored]
---

### Env var channels

Channel determines when a value goes live.

| Channel | Set with | When live |
|---|---|---|
| Service-level env | `zerops_env action="set"` | `restartedServices` lists cycled runtime containers; `restartedProcesses` has Process details. |
| `run.envVariables` | Edit `zerops.yaml`, commit, deploy | Full redeploy. `zerops_manage action="reload"` does NOT pick them up. |
| `build.envVariables` | Edit `zerops.yaml`, commit, deploy | Next build uses them; not visible at runtime. |

**Suppress restart**: pass `skipRestart=true`; response reports
`restartSkipped: true`, `nextActions` says how to restart, and the value
is **not live** until then. Partial failures land in `restartWarnings`;
`stored` confirms landed keys.

**Shadow-loop pitfall**: `zerops_env`-set service-level vars shadow
the same key in `run.envVariables`. Fixing only `zerops.yaml` won't
change live value — delete the service-level key
(`zerops_env action="delete"`) before redeploy.
