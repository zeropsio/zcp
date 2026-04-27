---
id: develop-first-deploy-scaffold-yaml
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Scaffold zerops.yaml for the first deploy"
references-fields: [ops.DiscoverResult.Services, workflow.ServiceSnapshot.Mode, workflow.ServiceSnapshot.StageHostname]
references-atoms: [develop-deploy-modes, develop-first-deploy-env-vars]
---

### Scaffold `zerops.yaml`

Scaffold `zerops.yaml` before the first deploy. Root placement and
`setup:` naming rules are in `develop-platform-rules-common`.

**Shape (one `zerops:` block per runtime hostname the plan targets):**

```yaml
zerops:
  - setup: <hostname>
    build:
      base: <runtime-only key, e.g. nodejs@22 — NOT the composite run key>
      buildCommands: [...]       # optional for pre-built artefacts
      deployFiles: [.]           # dev mode; stage uses the build output dir
    run:
      base: <run key, may be composite: php-nginx@8.4, nodejs@22, ...>
      ports:
        - port: <app-listens-on>
          httpSupport: true
      envVariables:
        <KEY>: <value or ${service_KEY} cross-ref>
      start: <run command, not a build command>
```

**Env var references** — see `develop-first-deploy-env-vars` for the
discovered-key catalog and the cross-service `${hostname_KEY}` syntax.

**Mode-aware tips:**

- `dev` mode: `deployFiles: [.]`, build runs on SSHFS, `run.start` wakes
  the runtime container — no stage pair to worry about.
- `simple` mode: identical layout to dev but single-slot, no stage.
- `standard` mode: emit separate entries for dev AND stage hostnames;
  stage's `deployFiles` points at the build output directory.

**Content-root tip:** for runtimes that expect assets at the
working-dir root (e.g. ASP.NET's `wwwroot/` lookup at
`/var/www/wwwroot/`), use **tilde-extract** (`./out/~`) so contents
land at `/var/www/` instead of `/var/www/out/`. Use **preserve**
(`./out`) when `run.start` references an explicit subpath like
`./out/app/App.dll`. Full decision rule in `develop-deploy-modes`.

Schema: fetch `zerops.yaml` JSON Schema via `zerops_knowledge` if unsure.
