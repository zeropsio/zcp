---
id: bootstrap-close
priority: 8
phases: [bootstrap-active]
steps: [close]
title: "Close bootstrap — hand off to develop"
---

### Closing bootstrap

Bootstrap is **infrastructure-only**. Closing seals the infra plan by
writing `ServiceMeta` records with `BootstrappedAt` for every planned
runtime; `FirstDeployedAt` stays empty — develop fills it on first deploy.

**What bootstrap guarantees at close:**

- Every planned service is provisioned on Zerops (managed services
  `RUNNING`, runtimes registered but not-yet-deployed).
- Dev containers are SSH-mount ready for the develop workflow.
- Env vars produced by managed services are discoverable and have been
  recorded in the session.

**What bootstrap does NOT do:**

- Write application code.
- Generate `zerops.yaml`.
- Deploy anything. Not even once.

**Next step — `zerops_workflow action="start" workflow="develop"`.** Develop runs the full code-and-deploy loop:

1. Scaffold `zerops.yaml` driven by the planned runtimes.
2. Write the application code that implements the user's intent.
3. Run the first deploy and verify — fresh services enter the
   first-deploy branch, which stamps `FirstDeployedAt` on the
   `ServiceMeta` when HTTP verification passes.
4. Iterate on failures — stop after 5 unsuccessful attempts and escalate to the user.

Direct tools (`zerops_scale`, `zerops_env`, `zerops_subdomain`,
`zerops_discover`) are always callable without a workflow wrapper — use
them for one-shot infra adjustments that don't warrant a session.

Complete this step before starting develop.
