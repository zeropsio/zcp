---
id: idle-develop-entry
priority: 1
phases: [idle]
idleScenarios: [bootstrapped]
title: "Develop entry"
---

**Start a develop workflow for every code change** — do not edit + deploy
directly:

```
zerops_workflow action="start" workflow="develop" intent="{task-description}"
```

The develop workflow tracks deploys and verifies, and auto-closes when
the task is complete.
