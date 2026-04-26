---
id: develop-first-deploy-intro
priority: 1
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "First-deploy branch — scaffold + write + deploy + stamp"
references-fields: [workflow.ServiceSnapshot.Deployed, ops.VerifyResult.Status]
references-atoms: [develop-first-deploy-scaffold-yaml]
---

### You're in the develop first-deploy branch

The envelope reports at least one in-scope service with
`deployed: false` (bootstrapped but never received code). Finish that
here: scaffold `zerops.yaml`, write the app, deploy, verify.

Flow for each never-deployed runtime:

1. **Scaffold `zerops.yaml`** from the planned runtime + env-var
   catalog from `zerops_discover` (see
   `develop-first-deploy-scaffold-yaml`).
2. **Write the application code** that implements the user's intent —
   not a placeholder, real code.
3. **Run `zerops_deploy targetService=<hostname>`** with NO `strategy`
   argument. Every first deploy uses the default push path;
   `strategy=git-push` requires `GIT_TOKEN` + committed code
   (container) or a configured git remote (local), neither ready yet.
4. **Verify** with `zerops_verify serviceHostname=<hostname>`. A
   passing verify combined with a recorded successful deploy flips
   the envelope's `deployed: true` on the next envelope build.

Don't skip to edits before the first deploy lands — SSHFS mounts can
be empty and HTTP probes return errors before any code is delivered.
