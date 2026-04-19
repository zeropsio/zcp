---
id: bootstrap-deploy-standard
priority: 5
phases: [bootstrap-active]
routes: [classic]
modes: [standard]
steps: [deploy]
title: "Bootstrap — standard mode deploy flow (dev + stage)"
---

### Standard mode — deploy flow

Prerequisites: import done, dev auto-mounted, env vars discovered,
code written to mount path.

**Core lifecycle** — deploy-first. Dev uses idle `start: zsc noop`;
no server auto-starts. The agent orchestrates:

1. `zerops_deploy` to dev — activates envVariables, runs build
   pipeline, persists files.
2. Start the server via SSH — env vars are now OS env vars.
3. `zerops_verify` dev — endpoints respond with real env values.
4. Generate the stage entry in zerops.yaml (prod setup). Dev is
   proven; now write the production config.
5. `zerops_deploy` stage from dev — stage auto-starts (real
   `start:` command).
6. `zerops_verify` stage.

Steps 1-3 repeat on iteration. Stage (steps 4-6) only after dev is
healthy.

**Concrete calls:**

```
zerops_deploy targetService="{hostname}" setup="dev"
# start server via new SSH
zerops_subdomain serviceHostname="{hostname}" action="enable"
zerops_verify serviceHostname="{hostname}"

# dev is healthy — add stage entry to zerops.yaml, then:
zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}" setup="prod"
zerops_subdomain serviceHostname="{stage-hostname}" action="enable"
zerops_verify serviceHostname="{stage-hostname}"
```

### Dev → Stage notes

- Stage has a real start command — it auto-starts after deploy.
  No SSH start needed. Zerops monitors via `healthCheck` and
  restarts on failure.
- Stage runs the full build pipeline; `buildCommands` may include
  compilation that dev didn't need.
- After deploy only `deployFiles` content persists. Anything
  installed manually via SSH is gone — use `prepareCommands` or
  `buildCommands` for runtime deps.
- Copy envVariables from dev — already proven via `/status`.
- **Shared storage** — if storage is in the stack, connect it to
  stage *after* the first deploy (stage was READY_TO_DEPLOY at
  import time, so `mount:` did not apply):
  `zerops_manage action="connect-storage" serviceHostname="{stage-hostname}" storageHostname="storage"`.
