---
id: develop-push-dev-workflow-simple
priority: 3
phases: [develop-active]
deployStates: [deployed]
modes: [simple]
strategies: [push-dev]
environments: [container]
title: "Push-dev iteration cycle (simple mode)"
references-atoms: [develop-platform-rules-container]
---

### Development workflow

Edit code on `/var/www/{hostname}/`. Redeploy for this mode (see
`develop-change-drives-deploy`); the runtime container auto-starts with its `healthCheck`:

```
zerops_deploy targetService="{hostname}" setup="prod"
zerops_verify serviceHostname="{hostname}"
```

Config-only changes still follow the same deploy path.
