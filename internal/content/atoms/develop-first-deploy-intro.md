---
id: develop-first-deploy-intro
priority: 1
phases: [develop-active]
deployStates: [never-deployed]
title: "First-deploy branch — scaffold + write + deploy + stamp"
---

### You're in the develop first-deploy branch

Bootstrap provisioned infrastructure but never wrote code or deployed
anything. At least one planned runtime has `bootstrapped: true` and
`deployed: false`. Finish the work here — the normal edit-loop branch
only opens after the first deploy verifies and `FirstDeployedAt` is
stamped.

Flow for each never-deployed runtime:

1. **Scaffold `zerops.yaml`** from the planned runtime + discovered env
   var catalog (see the dedicated atom below).
2. **Write the application code** that implements the user's intent —
   not bootstrap's placeholder, real code.
3. **Run the first deploy** via `zerops_deploy targetService=...` with
   NO `strategy` argument. The first deploy is always the default
   self-deploy mechanism, regardless of what eventual strategy the
   service will use. `strategy=git-push` requires `GIT_TOKEN` +
   committed code on the container, neither of which exists yet.
4. **Verify** with `zerops_verify` — a passing verify auto-stamps
   `FirstDeployedAt` on the ServiceMeta, exiting the first-deploy
   branch for the next session.

After the first deploy verifies, the next develop session will ask
you to confirm an ongoing strategy (`push-dev` / `push-git` /
`manual`). Skip straight to edits only after that first deploy
lands — SSHFS mounts may be empty, subdomains are disabled, and HTTP
probes fail before any code is delivered.
