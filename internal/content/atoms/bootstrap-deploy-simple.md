---
id: bootstrap-deploy-simple
priority: 5
phases: [bootstrap-active]
routes: [classic]
modes: [simple]
steps: [deploy]
title: "Bootstrap — simple mode deploy flow"
---

### Simple mode — deploy flow

Prerequisites: import done, service running, env vars discovered,
code written locally (simple mode still uses SSHFS; the mount is
auto-configured at provision).

Simple services have a real `start:` command — the container
auto-starts after deploy. No manual SSH start, no iteration-on-dev
cycle.

1. `zerops_deploy targetService="{hostname}" setup="prod"` — runs
   build, deploys, auto-starts the server.
2. `zerops_subdomain serviceHostname="{hostname}" action="enable"`
   — returns the subdomain URL.
3. `zerops_verify serviceHostname="{hostname}"` — must return
   status=healthy. If unhealthy, iterate: fix code, redeploy,
   re-verify.

Simple mode keeps the operational surface small: one container,
one deploy, server auto-starts. Use it for workers, cron jobs, or
anything that doesn't need dev/stage separation.

### Simple vs dev

- Simple auto-starts; dev stays idle for SSH iteration.
- Simple deploys only `deployFiles` content; dev uses `[.]` so
  source survives.
- Simple has a `healthCheck`; dev deliberately has none.
- Simple redeploys to apply code changes; dev edits propagate over
  SSHFS and the server is restarted via SSH.
