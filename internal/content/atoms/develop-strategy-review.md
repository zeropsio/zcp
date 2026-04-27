---
id: develop-strategy-review
priority: 2
phases: [develop-active]
deployStates: [deployed]
strategies: [unset]
title: "Pick an ongoing deploy strategy"
---

### Pick an ongoing deploy strategy

The first deploy landed and verified. Before iterating, confirm how
future deploys should work:

- `push-dev` — ZCP drives direct deploys via `zerops_deploy` (zcli push
  from your workspace: dev container → stage, or local CWD →
  stage). Fast for tight iteration.
- `push-git` — source of truth moves to an external git remote; Zerops
  builds triggered by a webhook or GitHub Actions. Container push-git
  uses `GIT_TOKEN` on the project; local push-git uses your own git
  credentials.
- `manual` — **you** orchestrate every deploy. ZCP stays out of the
  deploy loop; close steps don't suggest `zerops_deploy`.

```
zerops_workflow action="strategy" strategies={"{hostname}":"push-dev"}
```

Confirm a strategy before iterating. No code changes before confirming —
the default keeps working, but pick explicitly so redeploys use the
right mechanism.
