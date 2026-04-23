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

This is the service's first deploy — it counts as deployed only after
`zerops_verify` passes.

**Notes specific to the first deploy:**

- The Zerops service container is empty until this call lands. Probing
  its subdomain or (in container env) SSHing into it before the deploy
  will either fail outright or return a platform placeholder. Run the
  deploy first, then inspect.
- `zerops_deploy` batches build + container provision + start. Expect
  30–90 seconds for dynamic runtimes; implicit-webserver runtimes
  (`php-nginx`, `php-apache`) take longer because NGINX is wired into
  the image build.
- If the deploy itself returns a non-zero exit, diagnose BEFORE
  iterating: `zerops_logs severity="error" since="10m"` surfaces the
  exact build or start failure. A second deploy on the same broken
  `zerops.yaml` burns another deploy slot without new information.

After deploy returns success, run verify (see the next atom). The
deploy handler activates the L7 subdomain automatically on first deploy
— the response carries `subdomainAccessEnabled: true` and
`subdomainUrl` once the route is live.
