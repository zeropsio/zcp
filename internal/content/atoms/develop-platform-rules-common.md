---
id: develop-platform-rules-common
priority: 2
phases: [develop-active]
title: "Platform rules"
references-atoms: [develop-env-var-channels, develop-first-deploy-env-vars]
---

### Platform rules

- **Runtime user is `zerops`, not root.** Package installs need `sudo`
  (`sudo apk add …` on Alpine, `sudo apt-get install …` on Debian/Ubuntu).
- **Deploy = new container.** Local files in the current runtime container are
  lost; only content covered by `deployFiles` survives across redeploys.
- **`zerops.yaml` lives at the repo root.** Each `setup:` block (e.g.
  `prod`, `stage`, `dev`) is deployed independently — these are canonical
  recipe names, NOT hostnames.
- **Build ≠ runtime container.** Runtime packages → `run.prepareCommands`;
  build-only packages → `build.prepareCommands`. Build-time tools may
  not exist at run time; see guide `deployment-lifecycle`.
- Env var live timing and cross-service syntax:
  `develop-env-var-channels` / `develop-first-deploy-env-vars`.
- Service config changes (shared storage, scaling, nginx fragments):
  use `zerops_import` with `override: true` to update existing services.
  This is separate from `zerops_deploy`, which only updates code.
