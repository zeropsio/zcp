---
id: bootstrap-classic-verify-deploy-dynamic
priority: 4
phases: [bootstrap-active]
routes: [classic]
runtimes: [dynamic]
steps: [deploy]
title: "Classic — deploy verification server"
---

### Deploy the verification server

For each dynamic runtime in the plan, deploy a minimal hello-world server
that exposes `/status` on the declared port. This proves the service is
reachable from the Zerops network before application code is written.

Deploy pattern (per runtime):

```
zerops_deploy targetService="{hostname}" setup="dev"
zerops_verify  serviceHostname="{hostname}"
```

The verification server is thrown away at develop time — `workflow=develop`
replaces it with the real application. Do not invest in its features; the
only success criterion is HTTP 200 on `/status` from within the project's
private network.
