---
id: develop-close-mode-auto-standard
priority: 7
phases: [develop-active]
deployStates: [deployed]
modes: [standard]
closeDeployModes: [auto]
environments: [container]
multiService: aggregate
title: "Close task — close-mode=auto, standard mode"
references-atoms: [develop-auto-close-semantics, develop-dynamic-runtime-start-container]
---

### Closing the task

Deploy dev first, start the dev server, verify, then promote to stage. Run per dev/stage pair in scope:

```
{services-list:zerops_deploy targetService="{hostname}" setup="dev"
zerops_dev_server action=start hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"
zerops_verify serviceHostname="{hostname}"

zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}" setup="prod"
zerops_verify serviceHostname="{stage-hostname}"}
```

<!-- axis-o-keep: conditional inspection — phrase is "If the dev server is already running" inside an `action=status`-then-decide flow, not a state assertion -->
Cross-deploy packages the dev tree into stage with no second build; stage has a real `run.start` + `healthCheck`, so it auto-starts (no `zerops_dev_server` on the stage side). The work session closes once both halves have a successful deploy + passing verify (`closeReason=auto-complete`). If the dev server is already running after a code-only change, run `action=status` first; if `running: true`, skip `action=start`.
