# Workspace import.yaml — standard mode

Recipes always use **standard mode**: each runtime target gets a `{name}dev` + `{name}stage` pair. The dev service runs immediately from an empty container (`startWithoutCode: true`), reaching RUNNING so SSHFS mounts and SSH execution land on a live container. The stage service stays in READY_TO_DEPLOY until the first cross-deploy from dev.

## Dev vs stage canonical properties

| Property | Dev (`{name}dev`) | Stage (`{name}stage`) |
|----------|-------------------|-----------------------|
| `startWithoutCode` | `true` | omit |
| `maxContainers` | `1` | omit (default) |
| `enableSubdomainAccess` | `true` | `true` |
| `verticalAutoscaling.minRam` | `1.0` for compiled runtimes | omit (default) |

## Exception — shared-codebase worker

A worker target whose `sharesCodebaseWith` is set (the research-step decision that this worker rides inside a host codebase) gets only `{name}stage`. The host target's dev container runs both processes via SSH, so a dev worker service would be a zombie container running the same code with no worker process started. Separate-codebase workers (`sharesCodebaseWith` empty, the default — including the 3-repo case where the runtime matches but the repo does not) get their own dev+stage pair from their own `{slug}-worker` repo.

## Serve-only targets — toolchain on dev, serve-only on stage

If the plan's target type is a serve-only base (`static`, `nginx`), the `{name}dev` service uses a toolchain-capable type — typically the same runtime the zerops.yaml's `build.base` will use (for example `nodejs@22` for a Vite/Svelte SPA). The serve-only base is a prod-only concern (the zerops.yaml's `setup: prod` sets `run.base: static`); the dev container needs a shell, a package manager, and the dev server binary. The `{name}stage` service keeps the plan's serve-only type because stage runs the prod setup via cross-deploy. Example: plan target `type: static` → `appdev: type: nodejs@22` + `appstage: type: static`.

## Workspace shape at provision

`zeropsSetup` and `buildFromGit` are deliverable-only fields — they belong in the six recipe imports finalize writes, not in the workspace import. Deploys from the workspace run through `zerops_deploy` with the `setup` parameter, which maps the service hostname to the zerops.yaml setup name (for example `targetService="appdev" setup="dev"`).

## Canonical skeleton

```yaml
services:
  - hostname: appdev
    type: <runtime>@<version>
    enableSubdomainAccess: true
    startWithoutCode: true
    minContainers: 1
    # verticalAutoscaling.minRam: 1.0 for compiled runtimes

  - hostname: appstage
    type: <runtime>@<version>
    enableSubdomainAccess: true

  - hostname: <db>
    type: postgresql@<version>
    mode: NON_HA
    priority: 10
```

Dev reaches RUNNING immediately; stage stays in READY_TO_DEPLOY until the first cross-deploy from dev.
