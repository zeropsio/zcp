---
id: develop-ready-to-deploy
priority: 2
phases: [develop-active]
modes: [dev, simple, standard]
environments: [container]
serviceStatus: [READY_TO_DEPLOY]
title: "Dev service at READY_TO_DEPLOY — bring it to ACTIVE first"
---

### Dev service in READY_TO_DEPLOY

A runtime service sitting at `READY_TO_DEPLOY` (rather than `ACTIVE`) was
created without `startWithoutCode: true` and has never been deployed.
Until it reaches ACTIVE, SSHFS mount and SSH are unavailable — no code
edits, no `zerops_deploy`, no server starts.

Two valid ways to move it forward:

1. **Deploy code to it via `zerops_deploy`.** The first deploy transitions
   the service `READY_TO_DEPLOY → BUILDING → ACTIVE`. Use this when the
   user's request already has code to ship. Write `zerops.yaml` first,
   then `zerops_deploy targetService="{hostname}"` — it handles git + push
   internally. Note the build base must be a **runtime-only** key
   (`php@8.4`, `nodejs@22`, `go@1`) — composite types like `php-nginx@8.4`
   belong only in `run.base`.

2. **Re-import with `startWithoutCode: true` and `override=true`.** Use
   this when you need the runtime container ACTIVE *before* deploying real code.
   Regenerate the import YAML with `startWithoutCode: true` on the
   target service and call `zerops_import content="<yaml>" override=true`
   — `override` replaces the existing service stack (without it the
   call fails with `serviceStackNameUnavailable`). Zerops then triggers
   an empty `stack.deploy`, lifting the service to ACTIVE.

Check `zerops_discover` first to see which state each runtime service is
in. `ACTIVE` means the dev loop is ready. `READY_TO_DEPLOY` means pick
one of the paths above before anything else.
