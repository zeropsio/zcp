---
id: develop-first-deploy-intro
priority: 1
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "First-deploy branch — scaffold + write + deploy + stamp"
references-fields: [workflow.ServiceSnapshot.Deployed, ops.VerifyResult.Status, workflow.ServiceSnapshot.CloseDeployMode]
references-atoms: [develop-first-deploy-scaffold-yaml, develop-first-deploy-write-app, develop-verify-matrix, develop-strategy-awareness]
---

### You're in the develop first-deploy branch

The envelope reports at least one in-scope service with
`deployed: false` (bootstrapped but never received code). Finish that
here: establish `zerops.yaml` and the app, deploy, verify.

Flow for each never-deployed runtime:

1. **Establish `zerops.yaml`** — scaffold if absent, refine in place if
   already present (see `develop-first-deploy-scaffold-yaml`).
2. **Establish the application code** — adapt existing source if the
   mount carries it, scaffold real code otherwise (see
   `develop-first-deploy-write-app`).
3. **Run `zerops_deploy targetService=<hostname>`** with NO `strategy`
   argument. Every first deploy uses the default push path;
   `strategy=git-push` requires `GIT_TOKEN` + committed code
   (container) or a configured git remote (local), neither ready yet.
4. **Verify** (see `develop-verify-matrix` for per-service path). Close
   and completion semantics are in `develop-auto-close-semantics`.

Auto-close is gated on `closeDeployMode` being set for every in-scope
service — `unset` blocks the close even after deploy + verify pass.
The Services block names each service's current value (`closeMode=auto|
git-push|manual|unset`); `unset` reads from a bootstrap that didn't
declare a strategy. Set it for each in-scope service:

```
zerops_workflow action="close-mode" closeMode={"<host>":"auto"}
```

`develop-strategy-awareness` covers all three axes (closeMode,
gitPush, buildIntegration) and the per-service mix.

Don't skip to edits before the first deploy lands — HTTP probes
return errors before any code is delivered.
