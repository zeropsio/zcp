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

Record one row per service in the provision attestation. Keys are enough — values stay redacted; discovery is for cataloguing, not consumption. The develop response covers per-service canonical key names plus cross-service reference syntax (`${hostname_varName}`) when wiring `run.envVariables` at first deploy.

**Adopt route — skip when no new wiring:** adopted services already carry their env wiring in the running app, so this discovery is only needed if THIS task adds NEW cross-service references. For a code-only change to an already-wired app (edit / redesign / bugfix), skip it and fetch keys lazily at wiring time — running it now is a no-op round-trip.

**Pre-first-deploy caveat (classic route)**: classic creates runtime services with `startWithoutCode: true` so they reach RUNNING before any code lands; env vars in such containers live in the project catalogue, not `process.env`, until develop runs the first deploy and references fire. Adopted services are usually ACTIVE; if `zerops_discover` shows `status=READY_TO_DEPLOY` the service was created without `startWithoutCode: true` — deploy fails until it reaches ACTIVE. Recovery: re-import with `startWithoutCode: true` + `override: true`. **Destructive**: override REPLACES the existing service stack; back up any uncommitted code first.
