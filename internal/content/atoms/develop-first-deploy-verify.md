---
id: develop-first-deploy-verify
priority: 5
phases: [develop-active]
deployStates: [never-deployed]
runtimes: [dynamic, implicit-webserver]
multiService: aggregate
title: "First deploy — verify rules"
references-fields: [ops.VerifyResult.Status, ops.VerifyResult.Checks, ops.CheckResult.Status, ops.CheckResult.Detail]
references-atoms: [develop-auto-close-semantics, develop-verify-matrix, develop-dynamic-runtime-start-container]
---

### Before verify on dev-mode dynamic runtimes

Dev-mode dynamic runtimes deploy with `start: zsc noop --silent` (a
no-op keepalive) — nothing is listening yet. `zerops_verify` will return
`http_root: HTTP 502` and that is NOT a deploy failure. Start the dev
process via `zerops_dev_server action=start` first, then verify.

For simple-mode and standard-mode runtimes the runtime starts on
deploy; verify directly.

### Verify the first deploy

After running `zerops_verify`, the returned `status` is `healthy`,
`degraded`, or `unhealthy`; scan `checks[]` for any with `status: fail`
and read its `detail` for the specific failure. The verify flow picks
the right check route per service shape (web / worker / managed).

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

Run for each runtime that hasn't been deployed:

```
{services-list:zerops_verify serviceHostname="{hostname}"}
```
