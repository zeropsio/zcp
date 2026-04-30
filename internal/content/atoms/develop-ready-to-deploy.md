---
id: develop-ready-to-deploy
priority: 2
phases: [develop-active]
modes: [dev, simple, standard, local-stage]
environments: [container]
serviceStatus: [READY_TO_DEPLOY]
title: "READY_TO_DEPLOY — bring to ACTIVE first"
---

### READY_TO_DEPLOY runtime

A runtime at `READY_TO_DEPLOY` lacks `startWithoutCode: true` and has
never deployed. Until ACTIVE, SSH and SSHFS into this service fail and
any `zerops_deploy` that would SSH-source from it fails.

Bring it to ACTIVE first by re-importing with `startWithoutCode: true`:
regenerate the import YAML setting `startWithoutCode: true` on the
target runtime, then `zerops_import content="<yaml>" override=true`.
Without `override` the call fails with `serviceStackNameUnavailable`.
Zerops replaces the service and lifts it to ACTIVE via empty
`stack.deploy`. Then proceed with the normal first-deploy.

Check `zerops_discover` first. `ACTIVE` is ready; `READY_TO_DEPLOY`
means re-import before anything else.
