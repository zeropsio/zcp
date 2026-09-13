---
id: develop-failed-build-recover
priority: 2
phases: [develop-active, bootstrap-active]
modes: [dev, simple, standard, local-stage]
environments: [container]
serviceStatus: [READY_TO_DEPLOY, FAILED]
deployHistory: [failed]
title: "READY_TO_DEPLOY/FAILED with failed deploy history — fix and redeploy, never override"
coverageExempt: "rare recovery atom — fires only when serviceStatus is READY_TO_DEPLOY/FAILED AND deployHistory=failed (a prior deploy attempt exists and failed). This is the <1%-session recovery path for a service holding failed deploy history"
---

### Failed deploy history — repair, don't replace

This service already has a failed deploy attempt on record — it holds
real source and build history, unlike a never-deployed
`READY_TO_DEPLOY` service. Read `zerops_events serviceHostname="{hostname}"`
for the failed appVersion: `failureClass`, `failureCause`, and the
suggested action are the diagnosis. Fix the cause in the mounted
source, then `zerops_deploy targetService="{hostname}"` again.

The redeploy is never gated — the previous appVersion keeps serving
while you fix and retry, so there is no rush and no destructive
confirmation to acknowledge. A re-import with the destructive override
flag is **not** a repair for this shape: it replaces the service stack
and the mounted code you need to fix, wiping the exact evidence the
diagnosis depends on. Reserve that override path for a never-deployed
(`deployHistory: none`) target only.
