---
id: develop-first-deploy-scaffold-yaml
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Scaffold or refine zerops.yaml"
references-fields: [ops.DiscoverResult.Services, workflow.ServiceSnapshot.Mode, workflow.ServiceSnapshot.StageHostname]
references-atoms: [develop-deploy-modes, develop-first-deploy-env-vars]
---

### Establish `zerops.yaml`

Scaffold `zerops.yaml` if absent or refine it in place if already present.
Root placement and `setup:` naming rules are in `develop-platform-rules-common`.

**Shape (one `zerops:` block per targeted runtime hostname):**

```yaml
zerops:
  - setup: <hostname>
    build:
      base: <runtime-only key, e.g. nodejs@22 — NOT the composite run key>
      buildCommands: [...]       # optional for pre-built artefacts
      deployFiles: [...]         # see develop-deploy-modes for deployFiles per class
    run:
      base: <run key, may be composite: php-nginx@8.4, nodejs@22, ...>
      ports:
        - port: <app-listens-on>
          httpSupport: true
      envVariables:
        <KEY>: <value or ${service_KEY} cross-ref>
      start: <run command, not a build command>
```

**Env var references** — see `develop-first-deploy-env-vars` for
`${hostname_KEY}` syntax and `develop-env-var-channels` for live timing.

**Mode-aware tips:** emit separate setup entries per targeted hostname.
See `develop-deploy-modes` for deployFiles by deploy class.
