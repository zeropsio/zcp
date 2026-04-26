---
id: bootstrap-recipe-close
priority: 1
phases: [bootstrap-active]
routes: [recipe]
steps: [close]
title: "Recipe — auto-adopt on close"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.ServiceSnapshot.Strategy]
---

### Close the recipe bootstrap

Complete the close step:

```
zerops_workflow action="complete" step="close" attestation="Recipe bootstrapped — services active and verified"
```

After close, every service the recipe provisioned appears in the
envelope with `bootstrapped: true` and `strategy: unset` — strategy
is not chosen at bootstrap; develop picks it on first use.
`zerops_workflow action="status"` summarises the transition and
points at the primary follow-ups: `develop` (iterate on the code the
recipe provided) and `strategy` (configure deploy strategy via
`zerops_workflow action="strategy" strategies={hostname:value}` —
push-git triggers the central git deploy setup flow).
