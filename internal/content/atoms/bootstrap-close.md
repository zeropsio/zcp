---
id: bootstrap-close
priority: 8
phases: [bootstrap-active]
steps: [close]
title: "Close bootstrap — hand off to develop"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.ServiceSnapshot.Deployed]
---

### Closing bootstrap

Bootstrap is **infrastructure-only**. After you call
`action="complete" step="close"`, every planned runtime appears in the
envelope with `bootstrapped: true` — infrastructure is provisioned
(managed services `RUNNING`, runtimes registered, dev containers
SSH-mount-ready, managed env vars discoverable). For classic and
recipe-with-first-deploy-later routes the same services show
`deployed: false` and enter the develop first-deploy branch. For
adopt-route services and recipes that deployed during bootstrap the
envelope shows `deployed: true` directly.

No application code is written, no `zerops.yaml` generated, and no
deploy runs as part of bootstrap close itself.

**Next step — `zerops_workflow action="start" workflow="develop"`.**
Develop runs the full code-and-deploy loop:

1. Scaffold `zerops.yaml` driven by the planned runtimes.
2. Write the application code that implements the user's intent.
3. Run the first deploy and verify — services with `deployed: false`
   enter the first-deploy branch.
4. Iterate on failures — stop after 5 unsuccessful attempts and escalate to the user.

Direct tools (`zerops_scale`, `zerops_env`, `zerops_subdomain`,
`zerops_discover`) are always callable without a workflow wrapper — use
them for one-shot infra adjustments that don't warrant a session.

Complete this step before starting develop.
