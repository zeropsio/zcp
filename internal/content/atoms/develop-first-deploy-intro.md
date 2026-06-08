---
id: develop-first-deploy-intro
priority: 0
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
   already present.
2. **Establish the application code** — adapt existing source if the
   mount carries it, scaffold real code otherwise.
3. **Run `zerops_deploy targetService=<hostname>`** with NO `strategy`
   argument. Every first deploy uses the default push path;
   `strategy=git-push` requires `GIT_TOKEN` + committed code
   (container) or a configured git remote (local), neither ready yet.
4. **Verify** the service responds on its expected surface (web /
   worker / managed). Close and completion semantics fire once the
   close-mode is set and the deploy + verify pass.

Auto-close stays blocked while `closeDeployMode` is `unset` — the
DECISION section of this response carries the call to set it (it can
precede the first deploy).

Don't skip to edits before the first deploy lands — HTTP probes
return errors before any code is delivered.
