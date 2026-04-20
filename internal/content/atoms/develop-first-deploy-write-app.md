---
id: develop-first-deploy-write-app
priority: 3
phases: [develop-active]
deployStates: [never-deployed]
title: "Write the application code"
---

### Write the application code

Bootstrap under Option A does NOT ship a verification stub or hello-world
— `/var/www/{hostname}/` on the SSHFS mount is empty. The first deploy
only succeeds if real code is there.

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

Write code directly to `/var/www/{hostname}/` on the local SSHFS mount —
NOT via SSH into the container. SSH deploys blow away uncommitted
working-tree changes on container restart.
