---
id: bootstrap-env-var-discovery
priority: 3
phases: [bootstrap-active]
routes: [classic, adopt]
steps: [provision]
title: "Discover env vars before generate"
---

### Discover env vars during provision

Once newly-provisioned (classic) or newly-attached (adopt) services have reached RUNNING / ACTIVE, run discovery so the session records env-var KEYS for every managed service. This is authoritative — do not guess alternative spellings; unknown cross-service references become literal strings at runtime and fail silently.

```
zerops_discover includeEnvs=true
```

Record one row per service in the provision attestation. Keys are enough — values stay redacted; discovery is for cataloguing, not consumption. Develop atoms (`develop-first-deploy-env-vars`) cover per-service canonical key names + cross-service reference syntax (`${hostname_varName}`) — those fire when wiring `run.envVariables` at first deploy.

**Pre-first-deploy caveat (classic route)**: classic creates runtime services with `startWithoutCode: true` so they reach RUNNING before any code lands; env vars in such containers live in the project catalogue, not `process.env`, until develop runs the first deploy and references fire. Adopted services are typically already deployed, so this caveat doesn't apply on the adopt route.
