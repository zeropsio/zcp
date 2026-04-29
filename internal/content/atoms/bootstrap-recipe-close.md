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

After close, every service the recipe provisioned appears in the
envelope with `bootstrapped: true` and `closeMode: unset` —
close-mode is not chosen at bootstrap; develop picks it on first use.
`zerops_workflow action="status"` summarises the transition and
points at the primary follow-ups: `develop` (iterate on the code the
recipe provided) and `close-mode` (set per-service close behaviour
via `zerops_workflow action="close-mode" closeMode={"<hostname>":"auto|git-push|manual"}`).
Switching to `git-push` additionally requires `action="git-push-setup"`
to provision the remote.
