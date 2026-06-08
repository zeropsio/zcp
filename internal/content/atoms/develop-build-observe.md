---
id: develop-build-observe
priority: 5
phases: [develop-active]
modes: [standard, simple, local-stage, local-only]
closeDeployModes: [git-push]
buildIntegrations: [webhook, actions]
deployStates: [deployed]
multiService: aggregate
title: "Async build — failure triage when zerops_events surfaces a failed appVersion"
---
The git-push delivery pattern (push command, async-build framing, and
record-deploy on `Status=ACTIVE`) lives in the close-mode-git-push
guidance fired alongside this atom. Use this section when the watched
appVersion lands on a failure status instead.

## Failure statuses

When `zerops_events serviceHostname="<hostname>"` reports
`BUILD_FAILED`, `DEPLOY_FAILED`, or `PREPARING_RUNTIME_FAILED`, read
the failed event's `failureClass` (build / start / verify / network /
config / credential / other) + `failureCause` for the structured
diagnosis — same vocabulary the synchronous deploy path produces in
`DeployResult.FailureClassification`. That IS the diagnosis: a failed
build never started a runtime process, so `zerops_logs` is empty for
the service — read the event, not the log stream. Recovery is whatever
fixed the build (yaml change, missing env var, code issue) plus a
fresh push.
