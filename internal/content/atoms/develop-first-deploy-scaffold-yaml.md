---
id: develop-first-deploy-scaffold-yaml
priority: 2
phases: [develop-active]
envelopeDeployStates: [never-deployed]
title: "Scaffold or refine zerops.yaml"
references-fields: [ops.DiscoverResult.Services, workflow.ServiceSnapshot.Mode, workflow.ServiceSnapshot.StageHostname]
references-atoms: [develop-first-deploy-env-vars]
pointer-atoms: [develop-deploy-modes]
---

### Establish `zerops.yaml`

Scaffold `zerops.yaml` if absent or refine it in place if already
present. The file lives at the repo root; `setup:` matches the runtime
hostname (one `zerops:` entry per in-scope runtime).

**Shape (one `zerops:` block per targeted runtime hostname):**

```yaml
zerops:
  - setup: <hostname>
    build:
      base: <runtime-only key, e.g. nodejs@22 — NOT the composite run key>
      buildCommands: [...]       # optional for pre-built artefacts
      deployFiles: [...]         # [.] for self-deploy; build-output subset for cross-deploy
    run:
      base: <run key, may be composite: php-nginx@8.4, nodejs@22, ...>
      ports:
        - port: <app-listens-on>
          httpSupport: true
      envVariables:
        <KEY>: <value or ${service_KEY} cross-ref>
      start: <run command, not a build command>
```

**Env var references** use `${hostname_KEY}` syntax — Zerops rewrites
the placeholder at deploy time from the named service's catalog. Wrong
spelling stays literal and the app fails at connect.

**Mode-aware tips:** emit separate setup entries per targeted hostname.
`deployFiles` selects which build-container files land in the runtime:
- **Self-deploy** (single service, `sourceService == targetService`): MUST be
  `[.]` — narrower patterns overwrite and destroy the target's own source.
- **Cross-deploy** (dev → stage, `sourceService != targetService`): cherry-pick
  the build output — a dir path like `[./out]` keeps the dir, `[./out/~]` (tilde)
  extracts its contents to the deploy root.
