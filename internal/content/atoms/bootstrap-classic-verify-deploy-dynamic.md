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
exposing `/status` on the declared port.

```
zerops_deploy targetService="{hostname}" setup="dev"
zerops_verify  serviceHostname="{hostname}"
```

Only success criterion: HTTP 200 on `/status`. Do not invest in its
features — `workflow=develop` replaces it with real code.
