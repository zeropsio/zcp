---
id: idle-bootstrap-entry
priority: 1
phases: [idle]
idleScenarios: [empty]
title: "Bootstrap entry"
---

Start a bootstrap workflow to provision infrastructure:

```
zerops_workflow action="start" workflow="bootstrap" intent="{your-description}"
```

Keep the intent one sentence. The bootstrap conductor proposes a service
plan; you approve or adjust before any services are created.
