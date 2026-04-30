---
id: bootstrap-close
priority: 8
phases: [bootstrap-active]
steps: [close]
title: "Close bootstrap — hand off to develop"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.ServiceSnapshot.Deployed]
---

### Closing bootstrap

Bootstrap is **infrastructure-only**. After
`action="complete" step="close"`, planned runtimes show
`bootstrapped: true`: managed services are `RUNNING`, runtimes are
registered, dev containers are SSH-mount-ready, and managed env vars
are discoverable. Classic and recipe-with-first-deploy-later services
show `deployed: false` and enter develop's first-deploy branch. Adopted
services and recipes that deployed during bootstrap show `deployed: true`.

No application code is written, no `zerops.yaml` generated, and no
deploy runs as part of bootstrap close itself.

**Next step — `zerops_workflow action="start" workflow="develop"`.** Develop owns code, the first deploy, verify, iteration, and close-mode setup; `develop-first-deploy-intro` fires on entry for services with `deployed: false`.

Direct tools (`zerops_scale`, `zerops_env`, `zerops_subdomain`, `zerops_discover`) stay callable without a workflow wrapper for one-shot infra changes.

Complete this step before starting develop.
