---
id: develop-reserved-env-names
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Reserved env-var keys"
reference: true
---

### Reserved env-var keys

A few keys are platform-reserved in `zerops.yaml` `envVariables`, with two distinct failure shapes:

- **API-rejected at push** (`code: userDataUseOfSystemKey`, named inline by zcli): `hostname`, `PATH`, `serviceId`, `projectId`, `appVersionId`, `appVersionName`, `zeropsSubdomain`. Rename (`MY_HOSTNAME`) and retry.
- **Runtime-init crash** when set in `run.envVariables` — `HOSTNAME`, `Path`, `path` (anything colliding with `PATH`/`HOSTNAME` case-insensitively). Fine in `build.envVariables`. The symptom is the giveaway: `BUILD_FAILED` in 4-5s with **zero build logs**. Move to `build.envVariables` or rename (`APP_HOSTNAME`).

Platform-injected vars (`zeropsSubdomainHost`, `*CdnUrl`, `envIsolation`/`sshIsolation`) accept overrides but shadow the real value — override only with a reason. Common defaults (`USER`, `HOME`, `PORT`, `NODE_ENV`, …) are free to set.
