---
id: develop-first-deploy-verify
priority: 5
phases: [develop-active]
deployStates: [never-deployed]
title: "Verify the first deploy"
references-fields: [ops.VerifyResult.Status, ops.VerifyResult.Checks, ops.CheckResult.Status, ops.CheckResult.Detail]
references-atoms: [develop-auto-close-semantics]
---

### Verify the first deploy

```
zerops_verify serviceHostname="{hostname}"
```

The returned `status` is `healthy`, `degraded`, or `unhealthy`; scan
`checks[]` for any with `status: fail` and read its `detail` for the
specific failure. A passing verify corresponds to: service
`status=ACTIVE` in `zerops_discover`, HTTP 200 from the subdomain root
(or configured `/status`), and every declared env var present at
runtime.

**If unhealthy:**

1. Run `zerops_logs severity="error" since="5m"` — the start or
   request error is in the log.
2. Common first-deploy misconfigs, in frequency order:
   - App bound to `localhost` instead of `0.0.0.0`.
   - `run.start` invokes a build command rather than the entry point.
   - `run.ports.port` doesn't match what the app actually listens on.
   - Env var name drift — check `${hostname_KEY}` spelling against
     the discovered catalog.
3. Fix in place, redeploy, re-verify. Stop after 5 unsuccessful
   attempts and reassess.

Auto-close behavior is described in `develop-auto-close-semantics`.
