---
id: bootstrap-recipe-close
priority: 1
phases: [bootstrap-active]
routes: [recipe]
steps: [close]
title: "Recipe — auto-adopt on close"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.ServiceSnapshot.CloseDeployMode]
---

### Close the recipe bootstrap

Complete the close step:

```
zerops_workflow action="complete" step="close" attestation="Recipe bootstrapped — services active and verified"
```

After close, every service the recipe provisioned appears in the envelope with `bootstrapped: true` and `closeMode: unset`. Close-mode and git-push capability are configured in develop after the first deploy lands — `develop-strategy-review` surfaces the menu when actionable. Start develop next:

```
zerops_workflow action="start" workflow="develop"
```
