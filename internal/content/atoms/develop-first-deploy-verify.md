---
id: develop-first-deploy-verify
priority: 5
phases: [develop-active]
deployStates: [never-deployed]
title: "Verify the first deploy and stamp FirstDeployedAt"
---

### Verify the first deploy

```
zerops_subdomain serviceHostname="{hostname}" action="enable"
zerops_verify serviceHostname="{hostname}"
```

A passing `zerops_verify` marks the service deployed. Subsequent
sessions skip scaffold and enter the normal edit loop.

**What a passing verify requires:**

- Service `status=ACTIVE` per `zerops_discover`.
- HTTP 200 from the subdomain root (or the configured `/status`
  endpoint) with the expected body shape.
- Every declared env var present at runtime.

**If verify returns unhealthy:**

1. Run `zerops_logs severity="error" since="5m"` — the start or request
   error is in the log.
2. Common first-deploy misconfigs, in frequency order:
   - App bound to `localhost` instead of `0.0.0.0`.
   - `run.start` invokes a build command rather than the entry point.
   - `run.ports.port` doesn't match what the app actually listens on.
   - Env var name drift — check `${hostname_KEY}` spelling against the
     discovered catalog.
3. Fix in place, redeploy, re-verify. Stop after 5 unsuccessful
   attempts and reassess.

After the first verify passes, the service is deployed. The develop
session auto-closes when every in-scope service has a passing verify.
