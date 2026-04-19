# Zerops

Zerops is a PaaS with its own schema — not Kubernetes, Docker Compose, or
Helm. This file documents invariants that never change. For runtime guidance
about the current task, call `zerops_workflow action="status"`.

## Container Identity

- Containers run as user `zerops`, not root. Package installs use `sudo`
  (`sudo apk add …` on Alpine, `sudo apt-get install …` on Debian/Ubuntu).
- Build container and run container are separate. Packages needed at runtime
  (`ffmpeg`, `imagemagick`) belong in `run.prepareCommands`; packages needed
  only during build go in `build.prepareCommands`.

## Deploy Semantics

- Every deploy replaces the run container. Only files listed in `deployFiles`
  persist across deploys. Runtime edits, installed packages, `/tmp`, and
  logs are reset.
- `zerops.yaml` lives at the repo root. Each `setup:` section (e.g. `prod`,
  `stage`, `dev`) is deployed independently; the setup name is chosen at
  deploy time.

## Runtime Classes

- Dynamic runtimes (Node, Go, Python, Bun, Ruby, …) start with `zsc noop`.
  The real server must be started over SSH after each deploy. The workflow
  status surfaces the exact command when relevant.
- Static runtimes (php-apache, nginx) auto-start after deploy — no manual
  start step.
- Managed services (PostgreSQL, MariaDB, Redis/Valkey, KeyDB, RabbitMQ,
  NATS, object storage) have no deploy — scale and connect only.

## Cross-Service References

- Service-to-service references use `${hostname_varName}` inside
  `zerops.yaml` and import YAML. The `hostname` segment is the target
  service's hostname as declared at import time; `varName` is an env var
  the target service exposes (verify with `zerops_discover includeEnvs=true`).
- `envSecrets` in import YAML are auto-injected as runtime env vars —
  never re-reference them inside `run.envVariables`.

## Tool Surface

Begin every task with:

```
zerops_workflow action="status"
```

That tool returns the current phase, services, progress, and the concrete
next action. Every code task starts a develop workflow:
`zerops_workflow action="start" workflow="develop" intent="…"`.
