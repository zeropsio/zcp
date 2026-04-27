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
2. Deploy with `zerops_deploy targetService="{hostname}"` — `zerops_deploy`
   picks the right mechanism for the active strategy.
3. Verify the change via HTTP — open the project subdomain or fetch
   with curl. Do not tail `zerops_logs` for readiness; nginx is already
   serving the moment deploy lands.

**There is no SSH start step.** Static services have no long-running
process — nginx serves files as soon as the deploy lands.

**Build step** (Tailwind, bundler, SSG like Astro or Eleventy):
runs in the Zerops build container at deploy time. Local builds are
preview-only; Zerops rebuilds anyway.

**Strategy fit:** `manual` for low-traffic sites; `push-git` when the
site has CI; `push-dev` for fast iteration on a dev container over SSH.

A deploy that lands files but serves 404 / 403 is a `deployFiles` path
mistake, not a runtime failure.
