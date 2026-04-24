---
id: develop-first-deploy-execute
priority: 4
phases: [develop-active]
deployStates: [never-deployed]
title: "Run the first deploy"
---

### Run the first deploy

```
zerops_deploy targetService="{hostname}"
```

The Zerops container is empty until this call lands, so probing its
subdomain or (in container env) SSHing into it first will fail or hit
a platform placeholder — deploy first, then inspect. `zerops_deploy`
batches build + container provision + start; expect 30–90 seconds for
dynamic runtimes and longer for `php-nginx` / `php-apache`.

If `status` is non-success, read `buildLogs` / `runtimeLogs` /
`failedPhase` before retrying — a second attempt on the same broken
`zerops.yaml` burns another deploy slot without new information.

On first-deploy success the response carries `subdomainAccessEnabled:
true` and a `subdomainUrl` — no manual `zerops_subdomain` call is
needed in the happy path. Run verify next.
