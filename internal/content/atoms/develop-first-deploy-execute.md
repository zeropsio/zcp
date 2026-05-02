---
id: develop-first-deploy-execute
priority: 4
phases: [develop-active]
modes: [dev, simple, standard, local-stage]
deployStates: [never-deployed]
multiService: aggregate
title: "First deploy — execution rules"
references-fields: [ops.DeployResult.Status, ops.DeployResult.BuildLogs, ops.DeployResult.RuntimeLogs, ops.DeployResult.FailedPhase, ops.DeployResult.FailureClassification, ops.DeployResult.SubdomainAccessEnabled, ops.DeployResult.SubdomainURL]
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

On first-deploy success the response carries `subdomainAccessEnabled:
true` and a `subdomainUrl` — no manual `zerops_subdomain` call is
needed in the happy path. Run verify next.

Run for each runtime that hasn't been deployed:

```
{services-list:zerops_deploy targetService="{hostname}"}
```
