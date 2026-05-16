---
id: develop-reserved-env-names
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Reserved env-var keys"
references-atoms: [develop-first-deploy-env-vars, develop-env-var-channels]
---

### Reserved env-var keys — two failure modes

A small set of keys is platform-reserved and cannot be set in
`zerops.yaml` `envVariables`. Two distinct failure shapes; knowing
which one you hit tells you where to look.

### Regime 1 — Hard-reserved, API-level (any scope)

The Zerops API rejects these at push time with structured error
`code: userDataUseOfSystemKey`. zcli surfaces the error before upload
so you see it inline. Case-sensitive exact match:

- `hostname` (lowercase — Zerops' service-injected service name)
- `PATH` (uppercase only — `Path` and `path` fall under regime 2)
- `serviceId`, `projectId`, `appVersionId`, `appVersionName`
- `zeropsSubdomain` (the fully-resolved URL — `zeropsSubdomainHost`
  and `zeropsSubdomainString` are NOT in this list and ARE overridable)

If the API rejects, the error names which key failed. Rename the key
(`MY_HOSTNAME`, `MY_PATH`, etc.) and retry.

### Regime 2 — Run-scope-only, runtime-init crash

These pass the API check but break runtime container startup when set
in `run.envVariables`. They're fine in `build.envVariables`. The
pattern: anything that conflicts with `PATH` or `HOSTNAME`
case-insensitively at runtime-init.

- `HOSTNAME` (uppercase)
- `Path` (capitalized)
- `path` (lowercase)

Symptom: `BUILD_FAILED` event in 4-5 seconds with **zero build logs**
and a generic baseline cause. The deploy response carries no specific
hint at this layer; the empty-logs shape is the signal.

Move these to `build.envVariables` if you genuinely need the override
during the build phase, or rename the key entirely (`APP_HOSTNAME`,
`APP_PATH`).

### Regime 3 — Platform-provided, overridable

These vars are platform-injected as OS env vars but the API
silently accepts a user override. The override shadows the
platform-provided value; that is rarely what you want.

- `apiCdnUrl`, `staticCdnUrl`, `storageCdnUrl`
- `envIsolation`, `sshIsolation`
- `zeropsSubdomainHost`, `zeropsSubdomainString`

Override only when there's a specific reason (e.g. routing through
a custom CDN). Default is to read the value Zerops provides.

### Not reserved — feel free to set when needed

Common Linux/runtime defaults Zerops provides but the API does not
restrict you from overriding:

- `USER`, `HOME`, `LOGNAME`, `SHELL`, `PWD`
- `PORT` (number or quoted string — both work)
- `NODE_ENV`, `APP_ENV` and other framework mode flags
