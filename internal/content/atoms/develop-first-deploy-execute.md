---
id: develop-first-deploy-execute
priority: 4
phases: [develop-active]
modes: [dev, simple, standard, local-stage]
deployStates: [never-deployed]
multiService: aggregate
title: "First deploy — execution rules"
references-fields: [ops.DeployResult.Status, ops.DeployResult.BuildLogs, ops.DeployResult.RuntimeLogs, ops.DeployResult.FailedPhase, ops.DeployResult.FailureClassification, ops.DeployResult.PublicAccess]
---

### Run the first deploy

<!-- axis-o-keep: container is empty is universal at first-deploy (deployStates: [never-deployed] is the matching axis) -->
The Zerops container is empty until the deploy call lands, so probing
its subdomain or (in container env) SSHing into it first will fail or
hit a platform placeholder — deploy first, then inspect. `zerops_deploy`
batches build + runtime container provision + start. The call returns
when build completes; runtime container start is a separate phase
surfaced by `failureClassification.failedPhase` if it fails — read
that field rather than waiting on a fixed timeout.

If `status` is non-success, read `failureClassification` first — it
carries the matched `category`, `likelyCause`, and `suggestedAction`
distilled from the logs. Only fall through to `buildLogs` /
`runtimeLogs` when the classification is missing or its
`suggestedAction` doesn't match what you observe. A second attempt on
the same broken `zerops.yaml` burns another deploy slot without new
information.

The subdomain switches on once, automatically, the first moment the
platform can accept it — right after this deploy succeeds, for a
runtime whose listener is up by then. Read the response's `publicAccess`
field (`{intent, subdomain, url, domains}`): `subdomain: "on"` with a
`url` means it's live — no manual `zerops_subdomain` call needed. Once
switched off (by you or the user), it stays off — the one-time enable
never repeats. Run verify next.

If the bootstrap plan set `publicAccess: "none"` for this runtime,
it's internal-only by design: `publicAccess.subdomain` stays `"off"`
and no URL ever appears — `zerops_verify` proves reachability from
inside the project instead. To make an already-public runtime
internal-only after the fact, call `zerops_subdomain action="disable"`.

Run for each runtime that hasn't been deployed:

```
{services-list:zerops_deploy targetService="{hostname}"}
```
