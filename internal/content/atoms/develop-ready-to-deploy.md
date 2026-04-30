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
**Destructive**: override REPLACES the existing service stack — any
uncommitted work in `/var/www/<hostname>/` is gone after the new
(empty) container reattaches. Back up first, or write your `zerops.yaml`
to a non-mount path until the runtime is ACTIVE. The response Warnings
name the replaced hostnames.

After re-import + code deploy, if `zerops_verify` reports `http_root:
skip` "subdomain not enabled", run `zerops_subdomain action="enable"`
on the runtime as a one-shot fix and re-verify. Auto-enable can miss
the post-recovery deploy; the manual call covers it.

Check `zerops_discover` first. `ACTIVE` is ready; `READY_TO_DEPLOY`
means re-import before anything else.
