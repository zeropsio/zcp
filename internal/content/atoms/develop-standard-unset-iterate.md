---
id: develop-standard-unset-iterate
priority: 3
phases: [develop-active]
deployStates: [deployed]
modes: [standard]
closeDeployModes: [unset]
environments: [container]
multiService: aggregate
title: "Standard pair (close-mode unset) — dev iteration loop"
references-atoms: [develop-strategy-review, develop-standard-unset-promote-stage, develop-dynamic-runtime-start-container]
---

### Dev iteration loop (close-mode unset)

`develop-strategy-review` advises picking a close-mode before iterating, but the dev iteration steps are the SAME regardless of which mode you eventually pick — close-mode only changes what the *close* call does, not what the iteration looks like. While close-mode is `unset`, run the same per-iteration sequence on the dev half:

```
{services-list:zerops_deploy targetService="{hostname}" setup="dev"
zerops_dev_server action=start hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"
zerops_verify serviceHostname="{hostname}"}
```

After each iteration lands cleanly on the dev half, the stage half stays at adopt-time content until you cross-deploy — see `develop-standard-unset-promote-stage` for the dev → stage promotion. Auto-close stays blocked while close-mode is `unset` (per `develop-auto-close-semantics`); pick a close-mode (`auto`, `git-push`, or `manual`) via `develop-strategy-review` once you've confirmed the iteration loop works for this task.
