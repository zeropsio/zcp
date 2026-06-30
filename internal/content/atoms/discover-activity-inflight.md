---
id: discover-activity-inflight
priority: 2
phases: [idle]
idleScenarios: [adopt]
title: "A service can be building while its status reads READY_TO_DEPLOY"
references-fields: [ops.ServiceInfo.Activity, ops.ServiceActivity.Action, ops.ServiceActivity.ProcessID]
---

A service can be mid-build or mid-deploy while its status still reads
`READY_TO_DEPLOY`. `zerops_discover` carries a per-service `activity` object
whenever a build or deploy is live on it: a runtime doing its first deploy reads
`status:"READY_TO_DEPLOY"` with `activity:{action:"build", status:"BUILDING"}`
(or `action:"deploy", status:"DEPLOYING"`), passes through `CREATING`, and flips
to `ACTIVE` only once the deploy activates.

A service carrying `activity` is NOT idle — its first deploy is still running, so
adopting or deploying onto it now is premature. Wait until the field clears
(re-run `zerops_discover`, or watch `zerops_events serviceHostname=<svc>` until
the build reaches RUNNING/ACTIVE), then proceed. The `activity` object always
carries the live `processId`; a genuinely stuck process can be canceled with
`zerops_process processId=<id> action="cancel"`.
