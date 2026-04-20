---
id: bootstrap-verify
priority: 5
phases: [bootstrap-active]
steps: [close]
title: "Verify infrastructure is ready before handoff"
---

### Verify infrastructure before closing bootstrap

Bootstrap under Option A is infra-only: no code, no deploy, no HTTP
probe. Close must confirm the **platform layer** is healthy so the
develop workflow inherits a clean handoff.

```
zerops_discover
```

Required state for every planned service:

- Platform `status` = `RUNNING` for managed services (databases, caches,
  object storage). A managed service that never reached `RUNNING` means
  the import failed silently — investigate `zerops_process` logs, do
  not close.
- Runtime services may appear as `NOT_YET_DEPLOYED` — that is expected
  under Option A. Code and the first deploy happen in the develop
  workflow.
- Env vars discovered during provisioning must be recorded in the
  session so develop can wire them without re-discovering.

Do **not** run `zerops_verify` here — that tool probes the app layer
(HTTP reachability, `/status` endpoints) which only makes sense **after**
develop writes code and runs the first deploy. Running it during
bootstrap will report every runtime as failing and is noise.

If a managed service is stuck in a non-`RUNNING` state, bootstrap
hard-stops: surface the failure to the user rather than retrying —
infrastructure issues require the user's judgment.
