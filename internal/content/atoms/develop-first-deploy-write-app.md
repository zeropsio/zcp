---
id: develop-first-deploy-write-app
priority: 3
phases: [develop-active]
environments: [container]
envelopeDeployStates: [never-deployed]
title: "Write the application code"
references-fields: []
---

### Write the application code

Bootstrap does NOT ship a verification stub or hello-world — `/var/www/<hostname>/`
on the SSHFS mount is empty. The first deploy only succeeds if real code
is there.

**Checklist before deploying:**

1. **Code reads env vars from the OS at startup.** Never hardcode
   connection strings or host/port/credentials — bootstrap's discovered
   catalog is the authoritative source.
2. **App binds `0.0.0.0`** (not `localhost`/`127.0.0.1`). Zerops health
   checks call the service over the container's external interface; a
   loopback-bound app reports as healthy in tests but fails in
   `zerops_verify`.
3. **`run.start` invokes the production entry point.** Not `npm install`,
   not `build` — the start command must launch a long-running process.
4. **Observability hook** — implement `/status` or `/health` so
   `zerops_verify` has a deterministic endpoint to probe. Return 200 on
   success; embed a cheap dependency check (e.g. `SELECT 1` to the
   database) so a failing verify immediately tells you whether the
   problem is the app or the wiring.
5. **Audit "developer-friendly" framework defaults.** Iterative-dev
   frameworks (Streamlit, Gradio, Vite, Jupyter) are wrong-in-container
   in two ZCP-specific ways: push-dev creates `/var/www/.git` so any
   "auto-detect dev mode from parent `.git/`" heuristic mis-fires; and
   the runtime is behind L7 so `headless`/"reverse-proxy" framework
   flags need to be pinned to container-correct values. Pin each in
   the framework's **own config file** (CLI flags get lost on
   `run.start` rewrites). Don't suppress dev mode — fix the operational
   mismatch and keep hot-reload working.

**Write files** directly to `/var/www/<hostname>/` through the SSHFS
mount — Read/Edit/Write tools (and plain `rm`, `mv`, `cp` against mount
paths) all work because the mount bypasses container-side permissions
at the SFTP protocol level.

**Run commands** (`go build`, `php artisan`, `pytest`, framework CLIs,
dev server) via SSH into the container: `ssh <hostname> "cd /var/www
&& <command>"`. The reason is tool availability, not ownership — most
runtime-specific CLIs aren't installed on the ZCP host.

**Don't run `git init` from the ZCP-side mount.** Push-dev deploy
handlers manage the container-side git state — calling `git init` on
the SSHFS mount (`cd /var/www/<hostname>/ && git init`) creates
`.git/objects/` owned by root, which breaks the container-side
`git add` the deploy handler runs. Recovery if this already happened:
`ssh <hostname> "sudo rm -rf /var/www/.git"` — the next deploy
re-initializes it.
