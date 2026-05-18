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
- **Setup blocks (`prod`, `stage`, `dev`) are canonical recipe names,
  NOT hostnames.** Each block deploys independently.
- **Build ≠ runtime container.** Runtime packages → `run.prepareCommands`;
  build-only packages → `build.prepareCommands`. Build-time tools may
  not exist at run time; see guide `deployment-lifecycle`.
- Env vars use `${hostname_KEY}` syntax for cross-service references
  (Zerops rewrites at deploy from the named service's catalog). Local
  vars in `run.envVariables` shadow project-level entries with the
  same key.
- **`zerops_import override=true` is destructive** — REPLACES the
  service stack (container, code, env vars, filesystem). Reserved for
  explicit user-requested config changes (shared storage, scaling,
  nginx) that `zerops_deploy` can't handle. Never the default fix for
  hostname collisions, env drift, or unexpected state — pick a
  different hostname, adopt, or escalate. Back up first; Warnings
  name replaced hostnames.
