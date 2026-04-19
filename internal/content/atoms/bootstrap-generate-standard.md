---
id: bootstrap-generate-standard
priority: 4
phases: [bootstrap-active]
routes: [classic]
modes: [standard]
steps: [generate]
title: "Generate zerops.yaml — standard mode (dev+stage)"
---

### Standard mode (dev+stage) — zerops.yaml

Infrastructure verification only — write a hello-world server (`/`,
`/health`, `/status`), not the user's application. Write the dev
entry only now; the stage entry is added after dev is proven at
deploy time.

All files live at `/var/www/{hostname}/` — the SSHFS mount path
from provision.

**Dev setup rules:**

- `deployFiles: [.]` — ALWAYS, no exceptions.
- `start: zsc noop --silent` — container stays alive but idle.
  No server auto-starts; the agent starts it manually via SSH,
  keeping full control of the iteration cycle.
  **PHP runtimes (php-nginx, php-apache) omit `start:`.**
- `buildCommands:` — dependency installation only, no compilation.
  Source runs directly from `/var/www/`.
- **NO `healthCheck`** — the agent controls lifecycle manually. A
  health check would restart the container when the agent stops
  the server for iteration.

**Dev vs Prod reference** (stage entry uses prod rules after dev
is proven):

| Property | Dev | Prod / Stage |
|----------|-----------|------------------|
| Purpose | Iterate, debug, test | Final validation, production-like |
| `deployFiles` | `[.]` (entire source) | Runtime-specific build output |
| `start` | `zsc noop --silent` | Real binary / compiled start |
| `healthCheck` | None | `httpGet` on app port |
| `readinessCheck` | None | Optional |

**Mandatory pre-deploy check**:

- [ ] `setup: dev` entry exists (canonical recipe name, not
      hostname). Stage `setup: prod` comes later.
- [ ] `deployFiles: [.]` — no exceptions
- [ ] `run.start: zsc noop --silent` — implicit-web runtimes
      omit `start` entirely
- [ ] `run.ports` matches the app's listen port — implicit-web
      runtimes omit `ports` (port 80 fixed)
- [ ] `envVariables` only uses discovered names
- [ ] App binds `0.0.0.0:{port}`, not localhost
