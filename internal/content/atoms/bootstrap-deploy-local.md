---
id: bootstrap-deploy-local
priority: 5
phases: [bootstrap-active]
environments: [local]
routes: [classic]
steps: [deploy]
title: "Bootstrap — local deploy flow"
---

### Deploy — local mode

In local mode there is no SSHFS, no SSH-orchestrated dev container,
and no source-service concept. `zerops_deploy` pushes from the local
working directory into Zerops via `zcli push`; the build runs on
Zerops, not locally.

Deploy flow:

1. `zerops_deploy targetService="{hostname}" setup="prod"` — pushes
   local code, triggers the build pipeline on Zerops. Blocks until
   DEPLOYED or BUILD_FAILED.
2. `zerops_subdomain serviceHostname="{hostname}" action="enable"`
   — returns the subdomain URL.
3. `zerops_verify serviceHostname="{hostname}"` — must return
   status=healthy.
4. Present the URL + status to the user.

Key facts:

- **Deploy = new container on Zerops** — only `deployFiles` content
  persists. No mount, no SSH-side state carries over.
- **Local code is unchanged.** Edit locally, redeploy when ready.
- **Server auto-starts on Zerops** (real `start:` command +
  `healthCheck`) — no manual SSH start needed.
- **Subdomain persists** across redeploys — no need to re-enable
  after the first activation.
- **VPN connections survive deploys** — no reconnect needed.

If verify fails, diagnose with `zerops_logs severity="error"
since="5m"`, fix code locally, redeploy. Max 3 iterations before
escalating to the user.
