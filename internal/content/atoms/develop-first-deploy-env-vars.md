---
id: develop-first-deploy-env-vars
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Use the discovered env var catalog when wiring the app"
references-atoms: [develop-env-var-model]
pointer-atoms: [develop-reserved-env-names]
---

### Env var catalog from bootstrap

Managed services expose env var keys your runtime references. Fetch
the live key list per managed service with `zerops_discover
service="<hostname>" includeEnvs=true` and use those keys verbatim —
**do not guess alternatives**. The catalog is the authoritative source;
the host key is `hostname` (never `host`), other keys vary per service
type. Values are redacted by default — names suffice; pass
`includeEnvValues=true` only to troubleshoot.

Per-service env KEYS come from the live discover catalog above, never a
cheatsheet menu — use them verbatim. The SQL cheatsheet (SQL dep types
only) adds non-derivable gotchas, not keys. For exotic types,
`zerops_knowledge query="<service>"` returns the canonical page.

**Reserved keys — never set these in `envVariables`:** `hostname`, `PATH`,
`serviceId`, `projectId`, `appVersionId`, `appVersionName`, `zeropsSubdomain`
are rejected at push (zcli names the offender). `HOSTNAME` / `Path` / `path`
in `run.envVariables` crash runtime init — silent `BUILD_FAILED` in 4-5 s with
empty logs (they're fine in `build.envVariables`). Rename (`APP_HOSTNAME`).
