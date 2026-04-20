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

`zerops_verify` is the gate that stamps `FirstDeployedAt` on the
ServiceMeta — a passing verify is the signal that the first-deploy
branch completed. Subsequent develop sessions see `deployed: true` and
route into the normal edit-loop atoms instead of re-running scaffold.

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
3. Fix in place, redeploy, re-verify. 3-tier iteration ladder caps at
   5 attempts.

After the first verify passes, the service is deployed. The develop
session auto-closes when every in-scope service has a passing verify.
