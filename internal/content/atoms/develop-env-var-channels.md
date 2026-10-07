---
id: develop-env-var-channels
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Env var channels"
reference: true
references-fields: [tools.envChangeResult.Readers, tools.envChangeResult.RestartedServices, tools.envChangeResult.RestartWarnings, tools.envChangeResult.RestartSkipped, tools.envChangeResult.RestartedProcesses, tools.envChangeResult.Stored, tools.envChangeResult.ShadowWarnings]
---

### Env var channels

Channel determines when a value goes live.

| Channel | Set with | When live |
|---|---|---|
| Vault value (Shared or a service's own) | `zerops_env action="set"` | Restart of its readers: `readers` names the services whose deployed `run.envVariables` reference the key, `restartedServices` the ones cycled, `restartedProcesses` the Process details. |
| `run.envVariables` | Edit `zerops.yaml`, commit, deploy | Full redeploy. `zerops_manage action="reload"` does NOT pick them up. |
| `build.envVariables` | Edit `zerops.yaml`, commit, deploy | Next build uses them; not visible at runtime. |

**A vault value nobody references reaches no app.** An empty `readers`
list means nothing restarted — add the `NAME: ${KEY}` line to each
service that needs it, then deploy. A new line goes live with the
deploy; a changed value with the readers' restart.

**Suppress restart**: pass `skipRestart=true`; response reports
`restartSkipped: true`, `nextActions` names the readers to restart, and
the value is **not live** until then. Partial failures land in
`restartWarnings`; `stored` confirms landed keys.

**A line beats the vault.** A key a service's `run.envVariables` declares
can't also be stored in its own vault (`userDataDuplicateKey`) — edit
`zerops.yaml` + redeploy to change it. A Shared value of the same name
stores fine, but that service reads its line, not the Shared value —
`shadowWarnings` names the key + service and `nextActions` won't call it
live.
