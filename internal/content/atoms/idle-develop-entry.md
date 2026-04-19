---
id: idle-develop-entry
priority: 1
phases: [idle]
title: "Develop entry"
---

All services in this project are bootstrapped. Start a develop workflow for
every code change:

```
zerops_workflow action="start" workflow="develop" intent="{task-description}"
```

One develop workflow per coherent task. When the task is complete the
workflow auto-closes and you can start the next one.
