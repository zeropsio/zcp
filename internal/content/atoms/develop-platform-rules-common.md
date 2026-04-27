---
id: develop-platform-rules-common
priority: 2
phases: [bootstrap-active, develop-active]
title: "Platform rules — always applicable"
references-atoms: [develop-env-var-channels, develop-first-deploy-env-vars]
---

### Platform rules

- **Container user is `zerops`, not root.** Package installs need `sudo`
  (`sudo apk add …` on Alpine, `sudo apt-get install …` on Debian/Ubuntu).
- **Deploy = new container.** Local files in the running container are
  lost; only content covered by `deployFiles` survives across deploys.
- **`zerops.yaml` lives at the repo root.** Each `setup:` block (e.g.
  `prod`, `stage`, `dev`) is deployed independently — these are canonical
  recipe names, NOT hostnames.
- **Build ≠ run container.** Runtime packages → `run.prepareCommands`;
  build-only packages → `build.prepareCommands`. Tools available at
  build time may not be at run time. See guide `deployment-lifecycle`
  for the full split.
- `envVariables` in `zerops.yaml` are declarative — **not live**
  until a deploy. `printenv` before deploy returns nothing for them.
  Cross-service ref syntax + typo behavior:
  `develop-env-var-channels` / `develop-first-deploy-env-vars`.
- Service config changes (shared storage, scaling, nginx fragments):
  use `zerops_import` with `override: true` to update existing services.
  This is separate from `zerops_deploy`, which only updates code.
