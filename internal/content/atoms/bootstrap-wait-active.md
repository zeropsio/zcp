---
id: bootstrap-wait-active
priority: 3
phases: [bootstrap-active]
routes: [classic]
steps: [provision]
title: "Wait for services to reach a running state"
---

### Wait until services are running

After `zerops_import` completes, the Zerops engine provisions runtime containers
asynchronously. Subsequent deploy or verify calls against a service that is
still `CREATING` / `STARTING` will fail with a retryable error.

Poll service state:

```
zerops_discover
```

Repeat until every service reports a running status. Expected transitions: dev / simple runtimes → `RUNNING` (with `startWithoutCode: true`) or `ACTIVE` once a deploy lands; stage runtimes → `READY_TO_DEPLOY` (waiting for the first dev → stage cross-deploy); managed services → `RUNNING` / `ACTIVE`. The readiness predicate accepts BOTH `RUNNING` and `ACTIVE` as the operational state — do not block on a specific string. `READY_TO_DEPLOY` is acceptable for stage services in standard mode at this step.
