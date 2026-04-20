---
id: develop-strategy-review
priority: 2
phases: [develop-active]
deployStates: [deployed]
strategies: [unset]
title: "Pick an ongoing deploy strategy"
---

### Pick an ongoing deploy strategy

The first deploy landed and verified. The initial deploy uses the
default mechanism (self-deploy from the dev container, or local push
through `zerops_deploy`). Before iterating, confirm how future deploys
should work:

- `push-dev` — keep the current mechanism. Fast for tight iteration
  against the dev container. Recommended unless you need CI.
- `push-git` — move source of truth to an external git remote; Zerops
  picks up remote pushes. Requires a `GIT_TOKEN` project env var and a
  one-time commit cycle on the container before it takes effect.
- `manual` — you orchestrate every deploy yourself. `zerops_deploy`
  calls stay valid but the flow makes no automatic assumptions.

```
zerops_workflow action="strategy" strategies={"{hostname}":"push-dev"}
```

This atom fires every develop session until a strategy is confirmed.
No code changes before confirming — the implicit default keeps
working, but strategy-specific atoms (git-push steps, close sequences)
only unlock after `action="strategy"` records the choice.
