---
id: develop-closed-auto
priority: 1
phases: [develop-closed-auto]
title: "Develop auto-closed — next step"
---

Every service in scope has a successful deploy + passed verify. The develop
session auto-closed; the work is durable in git and on the platform.

Close explicitly or start the next task:

```
zerops_workflow action="close" workflow="develop"
zerops_workflow action="start" workflow="develop" intent="{next-task}"
```

Until you explicitly close, new deploy attempts attach to this
already-completed session.
