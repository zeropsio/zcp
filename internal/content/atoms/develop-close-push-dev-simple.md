---
id: develop-close-push-dev-simple
priority: 7
phases: [develop-active]
deployStates: [deployed]
modes: [simple]
strategies: [push-dev]
title: "Close task — push-dev simple mode"
---

### Closing the task

Simple-mode services auto-start on deploy via their `healthCheck`:

```
zerops_deploy targetService="{hostname}" setup="prod"
zerops_verify serviceHostname="{hostname}"
```
