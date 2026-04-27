---
id: develop-strategy-awareness
priority: 5
phases: [develop-active]
strategies: [push-dev, push-git, manual]
title: "Deploy strategy — current + how to change"
references-fields: [workflow.ServiceSnapshot.Strategy, workflow.ServiceSnapshot.Trigger]
---

### Deploy strategy — current + how to change

Each runtime service in the envelope has a `strategy` field:
`push-dev` (direct deploy from your workspace), `push-git`
(push committed code to an external git remote — carries a
`trigger: webhook|actions|unset` sub-field), `manual` (you
orchestrate every deploy yourself), or `unset` (bootstrap-written
placeholder; develop picks one on first use). The rendered Services
block shows this as `strategy=push-dev|push-git|manual|unset`.

Switch at any time without closing the session:

```
zerops_workflow action="strategy" strategies={"{hostname}":"push-dev"}
```

Mixed strategies across services in one project are fine — each
service's strategy is independent in the envelope.
