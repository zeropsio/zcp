# Deno on Zerops

## Keywords
deno, typescript, javascript, ubuntu, zerops.yml, permissions

## TL;DR
Deno runtime. REQUIRES `os: ubuntu` (not available on Alpine). Use `deno.jsonc`. Bind `0.0.0.0` via `Deno.serve`.

### Base Image

Includes Deno runtime.

**OS**: `os: ubuntu` REQUIRED (not available on Alpine).

### Build Procedure

1. Set `build.base: deno@latest`, `build.os: ubuntu`
2. Permissions: `--allow-net --allow-env` minimum
3. Use `deno.jsonc`, not `deno.json`

### Binding

`Deno.serve({hostname: "0.0.0.0"}, handler)`

### Build Caching

Run `deno cache main.ts` in buildCommands to pre-download dependencies. Ensures deployments are deterministic.
Cache: deps in `~/.cache/deno` (auto-cached).

### Resource Requirements

**Dev** (cache/compile on container): `minRam: 0.5` — `deno cache` moderate peak.
**Stage/Prod**: `minRam: 0.25` — Deno runtime lightweight.

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container -- agent starts `deno run --allow-net --allow-env main.ts` manually via SSH for iteration)
**Prod deploy**: `deployFiles: [.]`, `start: deno run --allow-net --allow-env main.ts`
For compiled binaries: `deno compile --output app main.ts` -> `deployFiles: app`, `start: ./app`
