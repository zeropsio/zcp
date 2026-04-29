---
id: develop-strategy-awareness
priority: 5
phases: [develop-active]
closeDeployModes: [auto, git-push, manual]
title: "Deploy config — current axes + how to change"
references-fields: [workflow.ServiceSnapshot.CloseDeployMode, workflow.ServiceSnapshot.GitPushState, workflow.ServiceSnapshot.BuildIntegration]
---

### Deploy config — current axes + how to change

Each runtime service has three orthogonal deploy-config axes — the
rendered Services block shows them as
`closeMode=auto|git-push|manual gitPush=unconfigured|configured|broken|unknown buildIntegration=none|webhook|actions`:

- `closeMode` — what the develop close action does. `auto` runs
  `zerops_deploy` directly (zcli push); `git-push` commits + pushes
  to a configured remote so Zerops/CI builds; `manual` yields to
  you for orchestration. `unset` is the bootstrap-written
  placeholder that develop converts on first use.
- `gitPush` — capability state for the git-push path. `configured`
  means GIT_TOKEN + .netrc + remote URL are stamped; `unconfigured`
  / `broken` / `unknown` indicate setup is needed before
  `closeMode=git-push` can fire.
- `buildIntegration` — ZCP-managed CI shape. `none` (default),
  `webhook` (Zerops webhook drives the build), or `actions` (GitHub
  Actions workflow YAML). Requires `gitPush=configured`.

Switch any axis without closing the session — three actions, one per
axis:

```
zerops_workflow action="close-mode"  closeMode={"{hostname}":"auto"}
zerops_workflow action="git-push-setup" service="{hostname}" remoteUrl="..."
zerops_workflow action="build-integration" service="{hostname}" integration="webhook"
```

Mixed config across services in one project is fine — each
service's three axes are independent in the envelope.
