---
id: develop-static-workflow
priority: 2
phases: [develop-active]
runtimes: [static]
title: "Static runtime — develop workflow"
coverageExempt: "static-runtime workflow — 30 canonical scenarios use dynamic + implicit-webserver runtimes (the common cases). Static (nginx, plain static) is a smaller share of agent sessions; covered by Phase 5 quarterly live-eval if a static-runtime project surfaces"
---

### Static runtime — develop workflow

Static services have no runtime process to restart. The develop loop is:

1. Edit files.
2. Deploy with `zerops_deploy targetService="{hostname}"` — `zerops_deploy`
   picks the right mechanism for the active strategy.
3. Verify the change via HTTP — open the project subdomain or fetch
   with curl. Do not tail `zerops_logs` for readiness; nginx is already
   serving the moment deploy lands.

**There is no SSH start step.** Static services have no long-running
process — nginx serves files as soon as the deploy lands.

**Minimal `zerops.yaml` for plain HTML / no build step:**

```yaml
zerops:
  - setup: <hostname>
    build:
      base: nodejs@22
      deployFiles: [.]
    run:
      base: nginx@1.22
```

`buildCommands` is OPTIONAL — omit it entirely; do not add a no-op
`echo` defensively. `run.start`, `run.ports`, `run.envVariables`,
`run.healthCheck` do not apply (nginx auto-serves on Zerops's
managed port). `build.base` MUST be a real builder runtime
(`nodejs@22` is the convention even when there is no JS to build) —
Zerops rejects `static` / `nginx` as build bases (`unknown base`)
despite their presence in the schema enum.

**Build step** (Tailwind, bundler, SSG like Astro or Eleventy):
runs in the Zerops build container at deploy time. Local builds are
preview-only; Zerops rebuilds anyway.

**Close-mode fit:** `manual` for low-traffic sites; `git-push` when the
site has CI; `auto` for fast iteration.

A deploy that lands files but serves 404 / 403 is a `deployFiles` path
mistake, not a runtime failure.
