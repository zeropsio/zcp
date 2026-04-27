---
id: develop-ready-to-deploy
priority: 2
phases: [develop-active]
modes: [dev, simple, standard]
environments: [container]
serviceStatus: [READY_TO_DEPLOY]
title: "READY_TO_DEPLOY — bring to ACTIVE first"
---

### READY_TO_DEPLOY runtime

A runtime at `READY_TO_DEPLOY` lacks
`startWithoutCode: true` and has never deployed. Until ACTIVE, SSHFS and
SSH are unavailable — no code edits, no `zerops_deploy`, no server starts.

Two valid ways to move it forward:

1. **Deploy code via `zerops_deploy`.** If code is ready, write
   `zerops.yaml`, then `zerops_deploy targetService="{hostname}"`; the
   tool handles git + push. The first deploy moves
   `READY_TO_DEPLOY → BUILDING → ACTIVE`. Build base: runtime-only
   (`php@8.4`, `nodejs@22`, `go@1`), not composite
   `php-nginx@8.4` except in `run.base`.

2. **Re-import with `startWithoutCode: true` and `override=true`.** If
   runtime must be ACTIVE before code, regenerate import
   YAML with `startWithoutCode: true` on the target service and call
   `zerops_import content="<yaml>" override=true`; without `override`
   it fails with `serviceStackNameUnavailable`. Zerops replaces the
   service and runs an empty `stack.deploy`, lifting it to ACTIVE.

Check `zerops_discover` first. `ACTIVE` is ready; `READY_TO_DEPLOY`
means choose one path above before anything else.
