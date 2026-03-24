# Gleam on Zerops

## Keywords
gleam, erlang, beam, shipment, ubuntu, zerops.yml, gleam.toml

## TL;DR
Gleam REQUIRES `os: ubuntu` in both build AND run. Deploy erlang-shipment. Watch for version mismatch -- Zerops `gleam@1.5` is old.

### OS Requirement

`os: ubuntu` REQUIRED in both build AND run (not available on Alpine).

### Build Procedure

1. Set `build.base: gleam@latest`, `build.os: ubuntu`
2. `buildCommands: [gleam export erlang-shipment]`
3. `deployFiles: build/erlang-shipment/~` (tilde extracts release to /var/www/)
4. `run.start: ./entrypoint.sh run` -- Erlang shipment includes entrypoint.sh
5. Set `run.os: ubuntu`

### Version Warning

`gleam@1.5` on Zerops is old. Modern `gleam_stdlib` versions require Gleam >=1.14.0. If dependencies fail with version mismatch, pin older dependency versions in gleam.toml.

### JavaScript Target

JavaScript target needs Node.js runtime instead.

### Resource Requirements

**Dev** (compilation on container): `minRam: 1` — `gleam build` + erlang-shipment peak ~0.7 GB.
**Stage/Prod**: `minRam: 0.25` — BEAM VM lightweight for most apps.

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container -- agent starts `gleam run` manually via SSH for iteration)
**Prod deploy**: build erlang-shipment, deploy extracted, `start: ./entrypoint.sh run`
