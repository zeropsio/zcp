---
id: develop-platform-rules-common
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Platform rules"
reference: true
---

### Platform rules

- **Runtime user is `zerops`, not root.** OS package installs need `sudo` in
  BOTH `build.prepareCommands` AND `run.prepareCommands` (`sudo apk add …` on
  Alpine, `sudo apt-get install …` on Debian/Ubuntu). The distro is the OS
  prefix of the service type: `ubuntu/…` → `apt-get`, `alpine/…` → `apk`.
- **Deploy = new container.** Local files in the current runtime container are
  lost; only content covered by `deployFiles` survives across redeploys.
- **Setup-block names depend on origin:** a recipe pre-authors `dev`/`prod`
  — don't rename those to hostnames. Authoring `zerops.yaml` from scratch you
  choose the name (a `setup:` per runtime hostname is fine). Each block
  deploys independently.
- **Build ≠ runtime container.** Runtime packages → `run.prepareCommands`;
  build-only packages → `build.prepareCommands`. Build-time tools may
  not exist at run time; see guide `deployment-lifecycle`.
- **`zerops_import override=true` is destructive** — REPLACES the
  service stack (container, code, env vars, filesystem). Reserved for
  explicit user-requested config changes (storage, scaling,
  nginx) that `zerops_deploy` can't handle. Never the default fix for
  hostname collisions, env drift, or unexpected state — pick a
  different hostname, adopt, or escalate. Back up first; Warnings
  name replaced hostnames.
