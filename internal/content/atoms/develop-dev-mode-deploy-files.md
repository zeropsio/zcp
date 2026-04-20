---
id: develop-dev-mode-deploy-files
priority: 3
phases: [develop-active]
modes: [dev]
title: "Dev mode — deployFiles must be [.]"
---

On a `dev`-mode runtime service, `zerops.yaml` `deployFiles` MUST be `[.]`
(the whole tree). Dev containers skip the build step; only files listed in
`deployFiles` are shipped to the container. A narrower pattern silently
leaves the container with stale code and your deploy "succeeds" while the
app runs the previous revision.

Stage-mode services use a different `deployFiles` (typically the build
output) — this rule applies to dev mode only.
