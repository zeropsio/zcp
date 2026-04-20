# Workspace import.yaml — static frontends (type 2a)

Static-frontend recipes serve through the built-in Nginx on the serve-only type, but the dev container still needs a toolchain that can run the framework's dev server (Vite, webpack, etc.) over SSH. The workspace import splits the two concerns across dev and stage.

## Canonical type split

- `{name}dev`: the toolchain runtime (typically `nodejs@22` or `bun@1`) is the service type. A static/Nginx container has no shell, no package manager, and no Node interpreter — the dev server binary needs a runtime container.
- `{name}stage`: the plan's serve-only type (`type: static`) stays. Stage runs `setup: prod` via cross-deploy, which builds the bundle and lets Nginx serve it.
- `{name}dev` keeps `startWithoutCode: true` so the build container reaches RUNNING immediately.

`build.base` inside zerops.yaml is what describes the build runtime (`nodejs@22` or similar). The service type on dev is the same toolchain runtime, matched to `build.base`. The serve-only base is a zerops.yaml `run.base: static` concern on the prod setup, not a service-type concern on dev.

## Shape when the plan has no database (pure static-frontend)

Type 2a plans with no managed services provision the app dev/stage pair only — the workspace import contains no database, cache, queue, or storage service. Env-var discovery (`zerops_discover`) is skipped for these plans because there is no managed service to expose env vars.

## Canonical skeleton

```yaml
services:
  - hostname: appdev
    type: nodejs@22        # toolchain runtime, not static
    enableSubdomainAccess: true
    startWithoutCode: true
    minContainers: 1

  - hostname: appstage
    type: static           # serve-only type on stage
    enableSubdomainAccess: true
```

`appdev` reaches RUNNING on the toolchain runtime so the dev server can spawn over SSH; `appstage` waits for the first cross-deploy to bring the built bundle onto the static base.
