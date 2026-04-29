---
id: develop-close-mode-auto-workflow-simple
priority: 3
phases: [develop-active]
deployStates: [deployed]
modes: [simple]
closeDeployModes: [auto]
environments: [container]
title: "close-mode=auto iteration cycle (simple mode)"
references-atoms: [develop-platform-rules-container]
---

### Development workflow

Edit code on `/var/www/{hostname}/`. Redeploy for this mode (see
`develop-change-drives-deploy`); the runtime container auto-starts with its `healthCheck`:

```
zerops_deploy targetService="{hostname}" setup="prod"
zerops_verify serviceHostname="{hostname}"
```

Config-only changes still deploy; env-var live timing is in
`develop-env-var-channels`.
