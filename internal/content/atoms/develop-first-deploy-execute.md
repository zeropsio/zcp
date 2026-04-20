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

Under Option A this is the first deploy — the service transitions from
`bootstrapped` to `bootstrapped + deployed` only after a passing verify.

**Notes specific to the first deploy:**

- The container is empty until this call lands. Running `ssh` into the
  service or probing its subdomain before the deploy will either fail
  outright or return a platform placeholder. Run the deploy first, then
  open SSH.
- `zerops_deploy` batches build + container provision + start. Expect
  30–90 seconds for dynamic runtimes; implicit-webserver runtimes
  (`php-nginx`, `php-apache`) take longer because NGINX is wired into
  the image build.
- If the deploy itself returns a non-zero exit, diagnose BEFORE
  iterating: `zerops_logs severity="error" since="10m"` surfaces the
  exact build or start failure. A second deploy on the same broken
  `zerops.yaml` burns another deploy slot without new information.

After deploy returns success, immediately enable the subdomain and run
verify (see the next atom).
