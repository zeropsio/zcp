---
id: develop-strategy-unset
priority: 1
phases: [develop-active]
strategies: [unset]
title: "Strategy not set — choose before deploying"
---

### Strategy selection required

At least one runtime service has no deploy strategy confirmed. Before any
deploy can proceed, pick a strategy for each such service:

- `push-dev` — SSH self-deploy from the dev container (the default bootstrap
  strategy; no CI/CD wiring required).
- `push-git` — push committed code to an external git remote; Zerops picks
  up the change and runs the pipeline.
- `manual` — you control when and what to deploy; `zerops_deploy` is a
  no-op for these services.

Record the choice once per service:

```
zerops_workflow action="strategy" strategies={"{hostname}":"push-dev"}
```

No work session is opened until every runtime service has a confirmed
strategy. This gate prevents a half-configured project from accumulating
deploy attempts against the wrong transport.
