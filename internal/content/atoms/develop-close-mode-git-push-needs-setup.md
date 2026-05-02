---
id: develop-close-mode-git-push-needs-setup
priority: 2
phases: [develop-active]
modes: [standard, simple, local-stage, local-only]
closeDeployModes: [git-push]
gitPushStates: [unconfigured, broken, unknown]
deployStates: [deployed]
multiService: aggregate
title: "Close-mode is git-push but capability isn't ready — set it up first"
references-atoms: [develop-close-mode-git-push, setup-git-push-container, setup-git-push-local]
---

This service is on `closeDeployMode=git-push`, but the runtime's `gitPushState` is not `configured` — pushing now will be rejected by `zerops_deploy strategy="git-push"` pre-flight (PUSH_NOT_CONFIGURED).

Run the capability setup first; the env-aware setup atom will be returned synchronously with the walkthrough:

```
{services-list:zerops_workflow action="git-push-setup" service="{hostname}"}
```

Follow the returned setup walkthrough (sets `GIT_TOKEN`, configures the remote, stamps `GitPushState=configured`). `git-push-setup` is per-pair (one call from the dev half writes capability for both halves of a standard pair); once `gitPushState=configured`, the next develop response delivers the actual push command.

If the previous state was `broken` (a setup attempt left the artifact damaged), the setup walkthrough re-runs the token + remote stamp from scratch — no manual cleanup needed.
