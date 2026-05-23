---
id: develop-strategy-awareness
priority: 5
phases: [develop-active]
closeDeployModes: [auto, git-push, manual]
multiService: aggregate
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
  means the last `git-push-setup` probe **proved end-to-end auth**: the
  supplied token authenticates against the remote URL, project env carries
  `GIT_TOKEN` (sensitive), and the working tree's git config has its
  `origin` synced. `unconfigured` / `broken` indicate setup is
  needed before `closeMode=git-push` can fire (`broken` means a previously-
  configured token stopped working, e.g. PAT rotation).
- `buildIntegration` — ZCP-managed CI. `actions` (recommended for
  GitHub remotes; zero manual dashboard step), `webhook` (Zerops
  dashboard OAuth — fallback for GitLab / policy-constrained
  repos), or `none`. Requires `gitPush=configured`.

Switch any axis without closing the session — three actions, each
operating at a different scope:

- `close-mode` is **per-service** and accepts a multi-entry map: one call sets close-mode for any subset of services in one shot. For a standard pair, set both halves in the same call.
- `git-push-setup` and `build-integration` are **per-pair**: call only on the dev half (or single-runtime hostname). The handler rejects stage-half targets with `INVALID_PARAMETER` because both halves of a pair share the same git-push / build-integration capability stamped on the dev meta.

```
zerops_workflow action="close-mode" closeMode={"{hostname}":"auto"}
zerops_workflow action="git-push-setup" service="{hostname}" remoteUrl="..."
zerops_workflow action="build-integration" service="{hostname}" integration="actions"
```

Substitute `{hostname}` with the dev-half hostname (or single-runtime hostname). For a multi-service project, repeat each call once per dev-half service — never per stage-half.

Mixed config across services in one project is fine — each service's three axes are independent in the envelope.
