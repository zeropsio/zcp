---
id: bootstrap-generate-simple
priority: 4
phases: [bootstrap-active]
routes: [classic]
modes: [simple]
steps: [generate]
title: "Generate zerops.yaml — simple mode"
---

### Simple mode — zerops.yaml

Infrastructure verification only — write a hello-world server
(`/`, `/health`, `/status`), not the user's application. All files
live at `/var/www/{hostname}/`.

Simple-mode services auto-start after deploy; no manual SSH start
needed. Write a single entry with a REAL start command.

**Simple setup rules:**

- `deployFiles: [.]` — ALWAYS (self-deploy; the source must
  survive on the container filesystem).
- `start:` — **real start command** (`node index.js`,
  `bun run src/index.ts`, `./app`, etc.) — NOT `zsc noop`.
- `buildCommands:` — dependency installation, plus compilation for
  Go / Rust / Java.
- `healthCheck:` — **YES, required.** Zerops monitors the container
  and restarts on failure.

```yaml
zerops:
  - setup: prod
    build:
      base: {runtimeVersion}
      buildCommands: [<from runtime knowledge>]
      deployFiles: [.]
      cache: [<runtime-specific cache dirs>]
    run:
      base: {runtimeBase}
      ports:
        - port: {port}
          httpSupport: true
      envVariables:
        # Map discovered variables to app-expected names.
      start: {start-command}
      healthCheck:
        httpGet:
          path: /health
          port: {port}
```
