---
id: develop-platform-rules-common
priority: 4
phases: [develop-active]
title: "Platform rules — always applicable"
---

### Platform rules

- **Deploy = new container.** Local files in the running container are lost;
  only content covered by `deployFiles` survives across deploys.
- `envVariables` are declarative config, **not live** until a deploy. Never
  check them with `printenv` before deploying — they will not be set yet.
- Env-reference typos (the `$hostname_varName` dollar-bracket form) render
  as literal strings — the platform does NOT raise an error. Check
  references carefully.
- The build container is a different environment from the run container.
  Tools available at build time may not be available at run time.
- Service config changes (shared storage, scaling, nginx fragments): use
  `zerops_import` with `override: true` to update existing services. This
  is separate from `zerops_deploy`, which only updates code.
