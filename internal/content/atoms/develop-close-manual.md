---
id: develop-close-manual
priority: 7
phases: [develop-active]
deployStates: [deployed]
strategies: [manual]
title: "Close task — manual strategy"
---

### Closing the task

Code is ready. **Inform the user** that the changes are complete — they
decide when (and whether) to deploy on manual strategy.

Reference deploy commands (user runs on their schedule):

```
zerops_deploy targetService="{hostname}"
zerops_verify  serviceHostname="{hostname}"
```

For a dev → stage promotion (when a stage pair exists):

```
zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}"
```
