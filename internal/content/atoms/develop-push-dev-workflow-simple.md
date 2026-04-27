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

Edit code on `/var/www/{hostname}/`. After each set of changes deploy — the
container auto-starts with its `healthCheck`, no manual server start:

```
zerops_deploy targetService="{hostname}" setup="prod"
zerops_verify serviceHostname="{hostname}"
```

If only `zerops.yaml` changes are config-level (no code change), the deploy
still applies them.
