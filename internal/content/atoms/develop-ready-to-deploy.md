---
id: develop-ready-to-deploy
priority: 2
phases: [develop-active, bootstrap-active]
modes: [dev, simple, standard, local-stage]
environments: [container]
serviceStatus: [READY_TO_DEPLOY]
deployHistory: [none]
title: "READY_TO_DEPLOY — bring to ACTIVE first"
coverageExempt: "rare recovery atom — fires only when serviceStatus=READY_TO_DEPLOY AND deployHistory=none (never deployed). Bootstrap-with-startWithoutCode (the common case) leaves services ACTIVE; this atom is the <1%-session recovery for the misconfigured shape"
---

### READY_TO_DEPLOY runtime, never deployed

A runtime imported without `startWithoutCode: true` and without
`buildFromGit` never starts — it has no deploy history to lose. Until
ACTIVE, SSH and SSHFS into this service fail and any `zerops_deploy`
that would SSH-source from it fails.

For this never-deployed shape, re-importing with `startWithoutCode:
true` is the only path to ACTIVE: regenerate the import YAML setting
`startWithoutCode: true` on the target runtime, then `zerops_import
content="<yaml>" override=true`. Without `override` the call fails with
`serviceStackNameUnavailable`; with it, the call answers
`DIAGNOSIS_REQUIRED` first — read the returned `wouldDestroy` payload
and re-call with `confirmDestructive` (operation + acknowledgedTargets
matching what it names) before the replacement proceeds.
**Destructive**: override REPLACES the existing service stack — any
uncommitted work in `/var/www/<hostname>/` is gone after the new
(empty) container reattaches. Back up first, or write your `zerops.yaml`
to a non-mount path until the runtime is ACTIVE. The response Warnings
name the replaced hostnames.

Check `zerops_discover` first. `ACTIVE` is ready; `READY_TO_DEPLOY` with
no deploy history means re-import before anything else. A service at
`READY_TO_DEPLOY`/`FAILED` that already holds failed deploy history is
a different shape — never override it; see the failed-build recovery
guidance instead.
