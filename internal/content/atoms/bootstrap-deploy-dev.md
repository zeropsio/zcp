---
id: bootstrap-deploy-dev
priority: 5
phases: [bootstrap-active]
routes: [classic]
modes: [dev]
steps: [deploy]
title: "Bootstrap — dev mode deploy flow"
---

### Dev-only mode — deploy flow

Prerequisites: import done, service auto-mounted, env vars discovered,
code written to the mount path.

> **Path distinction** — the SSHFS mount `/var/www/{hostname}/` is a
> LOCAL path. Inside the container, code lives at `/var/www/`. Never
> pass the LOCAL mount path as `workingDir` to `zerops_deploy` — the
> container default `/var/www` is always correct.

1. **Deploy** — `zerops_deploy targetService="{hostname}" setup="dev"`
   (self-deploy). **Deploy creates a new container — every previous
   SSH session dies (exit 255).**
2. **Start the server** via a **new** SSH connection (Bash
   `run_in_background=true`). Implicit-web runtimes skip this step
   — they auto-start.
3. **Enable the subdomain** — `zerops_subdomain
   serviceHostname="{hostname}" action="enable"`.
4. **Verify** — `zerops_verify serviceHostname="{hostname}"` must
   return status=healthy.
5. **Iterate** if degraded/unhealthy. Diagnose → fix → redeploy →
   start → re-verify. Max 3 iterations before surfacing to the user.

After verify succeeds, present the subdomain URL. No stage deploy in
dev-only mode.

### Dev iteration cycle

- **Env vars are OS env vars after deploy.** Never hardcode or pass
  inline values.
- **Code lives on the SSHFS mount.** Watch-mode frameworks reload;
  others need manual restart.
- **Redeploy only when zerops.yaml changes** (envVariables, ports,
  buildCommands). Code-only changes need a server restart, not a
  redeploy.
