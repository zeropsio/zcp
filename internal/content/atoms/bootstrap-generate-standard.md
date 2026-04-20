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

Write the dev entry only now; the stage entry is added after dev is
proven at deploy time.

**Dev entry rules:**

- `deployFiles: [.]` — ALWAYS, no exceptions.
- `start: zsc noop --silent` — container stays alive but idle. The agent
  starts the real server over SSH. **PHP runtimes (php-nginx, php-apache)
  omit `start:`.**
- `buildCommands:` — dependency installation only, no compilation.
- **NO `healthCheck`** — the agent controls lifecycle manually. A health
  check would restart the container when the agent stops the server.

**Dev vs Prod reference** (stage entry uses prod rules after dev is proven):

| Property | Dev | Prod / Stage |
|----------|-----------|------------------|
| Purpose | Iterate, debug, test | Final validation, production-like |
| `deployFiles` | `[.]` | Runtime-specific build output |
| `start` | `zsc noop --silent` | Real binary / compiled start |
| `healthCheck` | None | `httpGet` on app port |
| `readinessCheck` | None | Optional |

**Mandatory pre-deploy check:**

- [ ] `setup: dev` entry (canonical name, not hostname)
- [ ] `deployFiles: [.]`
- [ ] `run.start: zsc noop --silent` — implicit-web runtimes omit `start`
- [ ] `run.ports` matches the app's listen port — implicit-web omit
- [ ] `envVariables` only uses discovered variable names
- [ ] App binds `0.0.0.0:{port}`, not localhost
