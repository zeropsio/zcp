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

Infrastructure verification only — write a hello-world server (`/`,
`/health`, `/status`), not the user's application. That comes in
the develop workflow.

All files live at `/var/www/{hostname}/` — the SSHFS mount path
wired up at provision. Write a single dev entry; no stage service
exists in this mode.

**Dev setup rules:**

- `deployFiles: [.]` — ALWAYS, no exceptions.
- `start: zsc noop --silent` — container stays alive but idle; the
  agent starts the server manually via SSH. **PHP runtimes
  (php-nginx, php-apache) omit `start:` entirely.**
- `buildCommands:` — dependency installation only, no compilation.
- **NO `healthCheck`** — the agent controls lifecycle manually.

**Mandatory pre-deploy check** (do not proceed until all pass):

- [ ] `setup: dev` entry (canonical recipe name, not the hostname)
- [ ] `deployFiles: [.]` — no exceptions
- [ ] `run.start: zsc noop --silent` — implicit-webserver runtimes
      omit `start` entirely
- [ ] `run.ports` matches what the app listens on — implicit-web
      runtimes omit ports (port 80 fixed)
- [ ] `envVariables` only uses variables discovered in provision
- [ ] App binds `0.0.0.0:{port}`, not localhost
