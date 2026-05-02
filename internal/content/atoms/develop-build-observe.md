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
`DeployResult.FailureClassification`. Recovery is whatever fixed the
build (yaml change, missing env var, code issue) plus a fresh push.

For full build-container output, tail `zerops_logs` per failing
service:

```
{services-list:zerops_logs serviceHostname="{hostname}" facility=application since=5m}
```

`zerops_events` accepts `since=<duration>` to limit the window if the
service has long history.
