---
id: discover-activity-inflight
priority: 2
phases: [idle]
idleScenarios: [adopt]
title: "A service can be building while its status reads READY_TO_DEPLOY"
references-fields: [ops.ServiceInfo.Activity, ops.LiveOp.Action, ops.LiveOp.Status, ops.LiveOp.ProcessID]
---

A service can be mid-build or mid-deploy while its status still reads a resting
value like `READY_TO_DEPLOY` or `NEW`. `zerops_discover` carries a per-service
`activity` LIST of every live operation on it — a service can run several at once
(a buildFromGit import enqueues a build AND a subdomain-enable together). A
runtime doing its first deploy reads `activity:[{action:"build",
status:"BUILDING", processId:...}]` (then `"deploy"/"DEPLOYING"`), and only flips
to `ACTIVE` once the deploy activates.

A service carrying any `activity` is NOT idle — adopting or deploying onto it now
is premature. Block until it is done with `zerops_process action="wait"
service=<hostname>`: it waits until the service has no live process, draining the
build, the deploy, and any queued op (the subdomain-enable sits PENDING behind
the build). Then re-run `zerops_discover`. Each op carries its `processId`; a
genuinely stuck one can be canceled with `zerops_process processId=<id>
action="cancel"`.
