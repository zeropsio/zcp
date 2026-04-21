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

Keep the intent one sentence. The first call returns a ranked list of
route options (recipe matches, adopt, classic) — pick one and call
start again with `route=...` to commit the session. A service plan is
then proposed for you to approve or adjust before any services are
created.
