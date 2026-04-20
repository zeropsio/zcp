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
3. **Run the first deploy** via `zerops_deploy targetService=...`.
4. **Verify** with `zerops_verify` — a passing verify auto-stamps
   `FirstDeployedAt` on the ServiceMeta.

Skipping straight to edits only works after the first deploy lands.
Until then, SSHFS mounts may be empty, subdomains are disabled, and
HTTP probes will fail because no code has been delivered.
