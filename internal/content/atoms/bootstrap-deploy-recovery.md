---
id: bootstrap-deploy-recovery
priority: 7
phases: [bootstrap-active]
routes: [classic, recipe]
steps: [deploy]
title: "Bootstrap — recovery and iteration patterns"
---

### Recovery and iteration

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| Build FAILED: "command not found" | Wrong `buildCommands` for runtime | Check recipe, fix build pipeline |
| Build FAILED: "module not found" | Missing dependency init | Add install step to `buildCommands` |
| App crashes: EADDRINUSE | Port conflict | Ensure app listens on the port declared in `zerops.yaml` |
| App crashes: connection refused | Wrong DB / cache host | Compare `envVariables` mapping with discovered var names |
| `/status`: "db: error" | Missing or wrong env var | Diff zerops.yaml `envVariables` against discovery output |
| HTTP 502 | Subdomain not activated | `zerops_subdomain action="enable"` |
| `curl` returns empty | App listens on localhost, not `0.0.0.0` | Set `HOST=0.0.0.0` in `envVariables` |
| HTTP 500 | App error | `zerops_logs` + framework log files on the mount path |

Max **3 iterations** per service. After that, stop, report the
diagnosis, and ask the user whether to continue — do NOT keep
throwing fixes at a broken configuration.

### Escalating tiers (iteration counter)

- Iterations 1–2: `zerops_logs severity="error" since="5m"` — fix
  the specific error, redeploy, re-verify.
- Iterations 3–4: systematic check — all env-var keys present
  (`zerops_discover includeEnvs=true`); `zerops.yaml envVariables`
  only references discovered names; app binds `0.0.0.0`;
  `deployFiles` correct per mode; `run.ports.port` matches actual
  listen port; `run.start` is the RUN command, not a build
  command.
- Iteration 5+: STOP. Summarize what was tried in each iteration,
  current error (`zerops_logs` + `zerops_verify`), and ask the
  user whether to continue or debug manually.
