---
id: develop-close-mode-auto-simple
priority: 7
phases: [develop-active]
deployStates: [deployed]
modes: [simple]
closeDeployModes: [auto]
title: "Close task — close-mode=auto, simple mode"
---

### Closing the task

Simple-mode services auto-start on deploy; close through the cadence in
`develop-change-drives-deploy`:

```
zerops_deploy targetService="{hostname}" setup="prod"
zerops_verify serviceHostname="{hostname}"
```
