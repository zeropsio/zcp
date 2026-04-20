---
id: develop-static-workflow
priority: 2
phases: [develop-active]
runtimes: [static]
title: "Static runtime — develop workflow"
---

### Static runtime — develop workflow

Static services have no runtime process to restart. The develop loop is:

1. Edit files locally, or on the SSHFS mount in container mode.
2. Deploy with `zerops_deploy targetService="{hostname}"` — the tool
   picks the right mechanism for the active strategy.
3. Verify the change via HTTP — open the project subdomain or fetch
   with curl. Do not tail `zerops_logs` for readiness; nginx is already
   serving the moment deploy lands.

**There is no SSH start step.** Any guidance elsewhere about
`ssh {hostname} 'cd /var/www && {start-command}'` does not apply to
this runtime — nginx is serving before SSH is even available.

**Build step** (Tailwind, bundler, SSG like Astro or Eleventy): the
build runs in the Zerops build container during deploy, not locally.
To preview built output before pushing, run the build locally
(`npm run build`) and open the output directory; Zerops will rebuild
anyway at deploy time.

**Strategy fit:** `manual` for low-traffic sites; `push-git` when the
site has CI; `push-dev` for fast iteration on a dev container over SSH.

A deploy that lands files but serves 404 / 403 is a `deployFiles` path
mistake, not a runtime failure.
