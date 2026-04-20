---
id: bootstrap-generate-local
priority: 4
phases: [bootstrap-active]
environments: [local]
routes: [classic]
steps: [generate]
title: "Bootstrap — local-mode generate addendum"
---

### Generate — local mode

Files are written to the current working directory — no SSHFS mounts, no
remote paths.

**Canonical setup names by mode:**

| Mode | `setup:` | Notes |
|------|------------|-------|
| Standard | `prod` | Stage is the deploy target; the user's machine is dev. |
| Simple | `prod` | Single service — production profile (real `start`, `healthCheck`). |
| Managed-only | Not needed | No runtime service on Zerops. |

**Standard-mode zerops.yaml (stage service):**

```yaml
zerops:
  - setup: prod
    build:
      base: {runtimeVersion}
      buildCommands: [<from runtime knowledge>]
      deployFiles: [<runtime-specific build output>]
      cache: [<runtime-specific cache dirs>]
    run:
      base: {runtimeBase}
      ports:
        - port: {port}
          httpSupport: true
      envVariables:
        DATABASE_URL: ${db_connectionString}
      start: {start-command}
      healthCheck:
        httpGet:
          port: {port}
          path: /health
```

The `start` command is a REAL run command — never `zsc noop` in local
mode. Local mode has no SSH-orchestrated dev container.
