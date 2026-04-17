# Zerops

## Non-obvious semantics (Zerops ≠ Kubernetes / Compose / Helm — own schema)

- **Every code task = one develop workflow.** Start before any code change:
  `zerops_workflow action="start" workflow="develop" intent="..."`.
- **Deploy = new container.** Only `deployFiles` content persists. Runtime edits,
  installed packages, `/tmp`, logs — all reset on the next deploy.
- **Containers run as the `zerops` user, not root.** `prepareCommands` need
  `sudo` for package installs (`sudo apk add …` / `sudo apt-get install …`).
- **Build container ≠ Run container.** Packages needed at runtime
  (e.g. `ffmpeg`, `imagemagick`) belong in `run.prepareCommands`, not
  `build.prepareCommands`.
- **Dev containers with dynamic runtimes** (Node, Go, Python, Bun, …) start
  with `zsc noop` — you must start the real server over SSH after every
  deploy. Static runtimes (php-apache, nginx) auto-start.

The MCP server injects a live **Lifecycle Status** block into every response —
trust it for session/deploy/verify state. This file is for the static semantics
above.
