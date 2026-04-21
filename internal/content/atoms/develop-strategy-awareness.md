---
id: develop-strategy-awareness
priority: 5
phases: [develop-active]
strategies: [push-dev, push-git, manual]
title: "Deploy strategy — current + how to change"
---

### Deploy strategy — current + how to change

Each runtime service has a confirmed deploy strategy shown in the Services
section (`strategy=push-dev|push-git|manual`). Strategy switches take effect
immediately — no session restart needed.

Switch at any time (no session close required):

```
zerops_workflow action="strategy" strategies={"{hostname}":"push-dev"}
```

Valid values: `push-dev` (SSH self-deploy from dev container), `push-git`
(push committed code to external git remote), `manual` (you orchestrate
every deploy yourself). Mix different strategies across services in one
project if needed — per-service metas track them independently.
