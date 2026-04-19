---
id: develop-env-var-channels
priority: 2
phases: [develop-active]
title: "Env var channels — when each one goes live"
---

### Env var channels

Two channels set env vars; each applies differently.

| Channel | Set with | Becomes live |
|---------|----------|--------------|
| Service-level env | `zerops_env action="set"` | Tool **auto-restarts** the affected service(s). Live within a few seconds. |
| `run.envVariables` | Edit `zerops.yaml`, commit, deploy | Full redeploy. `zerops_manage action="reload"` does NOT pick them up. |
| `build.envVariables` | Edit `zerops.yaml`, commit, deploy | Next deploy runs a new build. Not visible at runtime. |

**`zerops_env set` auto-restart**: `zerops_env action="set"` restarts
the affected service(s) itself — no separate reload needed. Pass
`skipRestart=true` only when you will deploy immediately anyway (the
deploy restarts the container too).

**Shadow-loop pitfall**: a service-level env var set via `zerops_env`
shadows the same key declared in `run.envVariables`. If you set
`DB_HOST` via `zerops_env` and later fix it in `zerops.yaml`, redeploys
will not change the live value — the service-level value still wins.
Delete the service-level key first (`zerops_env action="delete"`),
then redeploy.
