---
id: develop-close-push-dev-dev
priority: 7
phases: [develop-active]
modes: [dev]
strategies: [push-dev]
environments: [container]
title: "Close task — push-dev dev mode (no stage)"
---

### Closing the task

Dev mode has no stage pair. Deploy the single runtime container and verify:

```
zerops_deploy targetService="{hostname}" setup="dev"
# start the server via a NEW SSH session (the old ones died with the deploy)
zerops_verify serviceHostname="{hostname}"
```
