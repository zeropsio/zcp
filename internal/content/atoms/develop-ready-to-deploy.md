---
id: develop-ready-to-deploy
priority: 2
phases: [develop-active]
modes: [dev, simple, standard]
environments: [container]
title: "Dev service at READY_TO_DEPLOY — bring it to ACTIVE first"
---

### Dev service in READY_TO_DEPLOY

A runtime service sitting at `READY_TO_DEPLOY` (rather than `ACTIVE`) was
created without `startWithoutCode: true` and has never been deployed. Until
it reaches ACTIVE, SSHFS mount and SSH are unavailable — no code edits,
no `zerops_deploy`, no server starts.

Two valid ways to move it forward:

1. **Deploy code to it.** The first `zerops_deploy` transitions the service
   READY_TO_DEPLOY → BUILDING → ACTIVE. Use this when the user's request
   already has code ready. Write `zerops.yaml` first, then deploy —
   `zerops_deploy targetService="{hostname}"` handles git + push internally.

2. **Re-import with `startWithoutCode: true`.** Use this when you need the
   container ACTIVE *before* deploying real code (e.g. to SSH in, inspect
   env vars, or let the LLM iterate over SSHFS before the first real
   deploy). Generate an import.yaml with the same service definitions plus
   `startWithoutCode: true`, then call `zerops_import` — Zerops auto-
   triggers an empty stack.deploy right after create, lifting the service
   to ACTIVE with a running empty container.

Check `zerops_discover` first to see which state each runtime service is
in. `ACTIVE` means the dev loop is ready. `READY_TO_DEPLOY` means pick
one of the paths above before anything else.
