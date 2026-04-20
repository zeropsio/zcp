# Provision — create workspace services from the plan

Create all workspace services from the recipe plan. This phase follows the same pattern as bootstrap — dev/stage pairs for each runtime target, plus the shared managed services. Provision completes when every service is RUNNING (dev) or READY_TO_DEPLOY (stage), every dev mount is present, every dev codebase has a container-side `.git/` with an initial commit, and the env-var catalog for each managed service has been recorded on the provision attestation.

## Provision substeps (authoritative order)

1. **Generate the workspace import.yaml.** Standard mode for dynamic runtimes; serve-only (static/nginx) targets get a toolchain-capable dev type and keep the serve-only type on stage. Dual-runtime recipes additionally set project-level `DEV_*` + `STAGE_*` URL constants with the correct port suffixes. Framework secrets split between project-level (shared across services) and per-service `envSecrets`.
2. **Call `zerops_import`** with the generated content; wait for every service to reach RUNNING.
3. **Mount the dev filesystem** for each dev service via `zerops_mount` — one mount per codebase, driven by `sharesCodebaseWith`.
4. **Configure git and initialise each mount** from the container side — one SSH call per mount, covering config + init + initial commit.
5. **Run `zerops_discover includeEnvs=true`** once services are RUNNING, and catalog the exact env var keys every managed service exposes.

## Workspace import.yaml fields that apply at provision

The workspace import creates service shells inside an existing ZCP project. The fields that apply here:

- `hostname` (max 40 chars, `[a-z0-9]` only, immutable).
- `type` (`<runtime>@<version>`, pick the highest from `availableStacks`).
- `mode` (`HA` or `NON_HA`, managed services only, immutable).
- `priority` (int; databases and storage set `10` so they start first).
- `enableSubdomainAccess` (true for publicly reachable dev services).
- `startWithoutCode` (dev services only — container reaches RUNNING without a deploy).
- `minContainers` (dev services set `1` — SSHFS needs a single container).
- `objectStorageSize` (object-storage services only, in GB).
- `verticalAutoscaling` (runtime + managed DB/cache; compiled runtimes need higher dev `minRam`).

For exotic fields (buildFromGit, cache layers, per-environment overrides) fetch the schema on demand:

```
zerops_knowledge scope="theme" query="import.yaml Schema"
```

## Two distinct import.yaml use cases (disambiguation)

Provision writes a **workspace import** — one file, no `project:` section, `services:` only, no `zeropsSetup`, no `buildFromGit`, `startWithoutCode: true` on dev services. The finalize step writes six separate **recipe deliverable imports** — one per end-user environment, each carrying `project:` with `envVariables` and `zeropsSetup: dev` / `zeropsSetup: prod` plus `buildFromGit`. Provision owns the workspace shape; finalize owns the deliverable shape.
