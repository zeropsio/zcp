---
id: bootstrap-classic-generate-static
priority: 4
phases: [bootstrap-active]
routes: [classic]
runtimes: [static]
steps: [generate]
title: "Static runtime — zerops.yaml"
---

### Static runtime — zerops.yaml

Static services serve files from disk — nginx is always up, no app
process, no port binding, no `/status` health check.

**Omit** the fields you would write for a dynamic runtime:

- Omit `run.start` — leave the key out entirely.
- Omit `run.ports` — port 80 is fixed; the platform handles HTTP routing.
- Omit `run.healthCheck.httpGet` — nginx is up the moment deploy lands.

**Minimal shape — no build step:**

```yaml
zerops:
  - setup: prod
    build:
      base: static
      deployFiles: ./
    run:
      base: static
```

**Shape with a build step** (Tailwind compile, markdown → HTML, SSG
bundler, etc.) — put it in `build.buildCommands` and narrow `deployFiles`
to the build output:

```yaml
zerops:
  - setup: prod
    build:
      base:
        - nodejs@22
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - dist/~./
    run:
      base: static
```

The `~./` suffix on `dist/~./` deploys the *contents* of `dist/` at the
container root — without it, the site lives at `/dist/index.html`.

**Mode fit.** Static services fit `simple` (one public hostname). Pure
HTML + `dev` is an anti-pattern. `dev` applies only when the dev half
is a dynamic runtime building files for a static runtime (out of scope).
