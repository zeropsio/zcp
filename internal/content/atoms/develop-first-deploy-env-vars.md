---
id: develop-first-deploy-env-vars
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Use the discovered env var catalog when wiring the app"
references-atoms: [develop-env-var-model, develop-env-var-channels, develop-reserved-env-names]
---

### Env var catalog from bootstrap

Managed services expose env var keys your runtime references. Fetch
the live key list per managed service with `zerops_discover
service="<hostname>" includeEnvs=true` and use those keys verbatim —
**do not guess alternatives**. The catalog is the authoritative source;
the host key is `hostname` (never `host`), other keys vary per service
type. Values are redacted by default — names suffice; pass
`includeEnvValues=true` only to troubleshoot.

Per-managed-type key cheatsheets render for the dep types in THIS
project only. For exotic types, `zerops_knowledge query="<service>"`
returns the canonical page. Reserved-keys atom covers keys forbidden in
`envVariables` (`HOSTNAME` in run = `BUILD_FAILED` 4-5s, empty logs).
