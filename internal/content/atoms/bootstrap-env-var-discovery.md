---
id: bootstrap-env-var-discovery
priority: 3
phases: [bootstrap-active]
routes: [classic, adopt]
steps: [provision]
title: "Discover env vars before generate"
---

### Discover env vars during provision

After import, run discovery so the session records env-var KEYS for every managed service. This is authoritative — do not guess alternative spellings; unknown cross-service references become literal strings at runtime and fail silently.

```
zerops_discover includeEnvs=true
```

Record one row per service in the provision attestation. Keys are enough — values stay redacted; discovery is for cataloguing, not consumption. Develop atoms (`develop-first-deploy-env-vars`) cover per-service canonical key names + cross-service reference syntax (`${hostname_varName}`) — those fire when wiring `run.envVariables` at first deploy.

**Runtime container caveat**: env vars resolve at deploy time, not as OS env on a never-deployed `startWithoutCode: true` dev container. They live in the project catalogue, not `process.env`. Develop runs the first deploy; references fire then.
