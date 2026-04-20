---
id: bootstrap-generate-dev
priority: 4
phases: [bootstrap-active]
routes: [classic]
modes: [dev]
steps: [generate]
title: "Generate zerops.yaml — dev mode"
---

### Dev-only mode — zerops.yaml

Single dev entry; no stage service exists in this mode.

**Dev entry rules:**

- `deployFiles: [.]` — ALWAYS, no exceptions.
- `start: zsc noop --silent` — container stays alive but idle; the agent
  starts the server manually via SSH. **PHP runtimes (php-nginx,
  php-apache) omit `start:` entirely.**
- `buildCommands:` — dependency installation only, no compilation.
- **NO `healthCheck`** — the agent controls lifecycle manually.

**Mandatory pre-deploy check:**

- [ ] `setup: dev` entry (canonical name, not hostname)
- [ ] `deployFiles: [.]`
- [ ] `run.start: zsc noop --silent` — implicit-web runtimes omit `start`
- [ ] `run.ports` matches the app's listen port — implicit-web omit
- [ ] `envVariables` only uses discovered variable names
- [ ] App binds `0.0.0.0:{port}`, not localhost
