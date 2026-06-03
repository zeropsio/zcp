---
id: develop-env-var-channels
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Env var channels"
reference: true
references-fields: [tools.envChangeResult.RestartedServices, tools.envChangeResult.RestartWarnings, tools.envChangeResult.RestartSkipped, tools.envChangeResult.RestartedProcesses, tools.envChangeResult.Stored, tools.envChangeResult.ShadowWarnings]
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

**Layer precedence — yaml-baked > service > project.** A key baked by
`run.envVariables` can't be set at service scope (`userDataDuplicateKey`,
the two never coexist) — edit `zerops.yaml` + redeploy to change it. The
reverse is silent: a `project=true` set of a key some service bakes (or
sets at service scope) stores fine, but that service keeps its higher
value — `shadowWarnings` names the key + service and `nextActions` won't
call it live. Fix at the winning layer. A self-shadow (`KEY: ${KEY}`) is
different: one self-referential line, not two layers.
