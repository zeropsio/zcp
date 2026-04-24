---
id: develop-env-var-channels
priority: 2
phases: [develop-active]
title: "Env var channels — when each one goes live"
---

### Env var channels

Two channels set env vars, and the channel determines when the value
goes live.

| Channel | Set with | When live |
|---|---|---|
| Service-level env | `zerops_env action="set"` | Response's `restartedServices` lists hostnames whose containers were cycled; `restartedProcesses` has platform Process details. |
| `run.envVariables` | Edit `zerops.yaml`, commit, deploy | Full redeploy. `zerops_manage action="reload"` does NOT pick them up. |
| `build.envVariables` | Edit `zerops.yaml`, commit, deploy | Next build uses them; not visible at runtime. |

To suppress the service-level restart, pass `skipRestart=true` — the
response then reports `restartSkipped: true` and `nextActions` tells
you to restart manually before the value is live. Partial failures
surface in `restartWarnings`. Read `stored` to confirm exactly which
keys landed.

**Shadow-loop pitfall**: a service-level env var set via `zerops_env`
shadows the same key declared in `run.envVariables`. If you set
`DB_HOST` via `zerops_env` and later fix it in `zerops.yaml`,
redeploys will not change the live value. Delete the service-level
key first (`zerops_env action="delete"`), then redeploy.
