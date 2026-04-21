# Finalize — service key inventory (showcase)

The service keys that appear in a showcase recipe's `envComments[i].service` map are fixed by the env shape and the worker's `sharesCodebaseWith` value. Every service present in that env's import.yaml takes a comment; a missing key degrades quality and risks falling short of the comment depth ratio for that env.

## Worker codebase split

- **Shared-codebase worker** (`sharesCodebaseWith` set on the worker target): the worker runs in the host target's dev container on envs 0-1, so only `workerstage` appears alongside `appstage`.
- **Separate-codebase worker** (`sharesCodebaseWith` empty — the default, including the 3-repo case): both `workerdev` and `workerstage` appear on envs 0-1.

Envs 2-5 always collapse to single-slot keys (`worker`), independent of the codebase split.

## Full-stack showcase (single runtime + worker)

- **Envs 0-1, shared-codebase worker**: `"appdev"`, `"appstage"`, `"workerstage"`, plus every managed service (`"db"`, `"cache"`, `"storage"`, `"search"`, ...).
- **Envs 0-1, separate-codebase worker**: `"appdev"`, `"appstage"`, `"workerdev"`, `"workerstage"`, plus every managed service.
- **Envs 2-5**: `"app"`, `"worker"`, plus every managed service.

## API-first showcase (dual-runtime + worker)

- **Envs 0-1, shared-codebase worker**: `"appdev"`, `"appstage"`, `"apidev"`, `"apistage"`, `"workerstage"`, plus every managed service.
- **Envs 0-1, separate-codebase worker**: `"appdev"`, `"appstage"`, `"apidev"`, `"apistage"`, `"workerdev"`, `"workerstage"`, plus every managed service.
- **Envs 2-5**: `"app"`, `"api"`, `"worker"`, plus every managed service.

## Key rule summary

- Runtime services on envs 0-1: dev + stage pair (e.g. `appdev` + `appstage`).
- Runtime services on envs 2-5: single-slot base hostname (e.g. `app`).
- Managed services: base hostname (`db`, `cache`, ...) across every env.
- Worker 0-1 split: one or two keys per the `sharesCodebaseWith` rule above.

Supply a comment for every key present in the env's import.yaml. Running `generate-finalize` with missing keys leaves those services silently uncommented — read the rendered file after each finalize call and confirm every service carries its prose.
