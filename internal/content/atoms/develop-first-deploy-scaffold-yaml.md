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
present. The file lives at the repo root and holds `setup: dev` for a
dev service plus one setup per deployable part (`prod` when the repo has
one part); a stage deploys the setup its plan names.

**Shape (one `zerops:` entry per setup):**

```yaml
zerops:
  - setup: <dev or the part's name>
    build:
      base: <os>/<runtime>@<ver>   # same OS prefix as the service type from discover, e.g. ubuntu/nodejs@22; php builds with <os>/php@8.4
      buildCommands: [...]       # optional for pre-built artefacts
      deployFiles: [...]         # [.] for self-deploy; build-output subset for cross-deploy
    run:
      base: <os>/<run key>@<ver>   # same OS prefix as build; may differ in tech (php-nginx@8.4 vs php@8.4)
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

**Mode-aware tips:** `deployFiles` selects which build-container files land in the runtime:
- **Self-deploy** (single service, `sourceService == targetService`): MUST be
  `[.]` — narrower patterns overwrite and destroy the target's own source.
- **Cross-deploy** (dev → stage, `sourceService != targetService`): cherry-pick
  the build output — a dir path like `[./out]` keeps the dir, `[./out/~]` (tilde)
  extracts its contents to the deploy root.
