# Zerops Platform Model

How Zerops works — the mental model for understanding all Zerops configuration.

Zerops runs Linux containers (Incus) in VXLAN private networks. Build and run are separate containers — deployFiles is the only bridge. Three storage types: container disk, shared storage (NFS), object storage (S3/MinIO).

## Container Universe

Everything on Zerops runs in **full Linux containers** (Incus, not Docker). Each container has:
- Full SSH access, working directory `/var/www`
- Connected via VXLAN private network (per project)
- Addressable by service hostname (internal DNS)
- Own disk (persistent, grow-only)

Hierarchy: **Project > Service > Container(s)**. One project = one isolated network. Services communicate by hostname over this network.

**Two core plans** govern project-level resource allowances:

| | Lightweight | Serious |
|---|---|---|
| Build time | 15 hours | 150 hours |
| Backup storage | 5 GB | 25 GB |
| Egress | 100 GB | 3 TB |
| Infrastructure | Single container | Multi-container (HA) |

Upgrading from Lightweight to Serious costs $10 one-time, is irreversible, and causes approximately 35 seconds of network unavailability.

## The Two YAML Files

| File | Purpose | Scope |
|------|---------|-------|
| `import.yaml` | **Topology** -- WHAT exists | Services, types, versions, scaling, env vars |
| `zerops.yaml` | **Lifecycle** -- HOW it runs | Build, deploy, run commands per service |

These are separate concerns. `import.yaml` creates infrastructure. `zerops.yaml` defines what happens when code is pushed. A service can exist (imported) without any code deployed yet.

## Build/Deploy Lifecycle

```
Source Code
    |
+--------------------------------+
|  BUILD CONTAINER               |
|  - Starts with base image ONLY |
|  - prepareCommands: cached     |
|  - buildCommands: compile      |
|  - Output: deployFiles         |
+-----------+--------------------+
            | deployFiles = THE ONLY BRIDGE
            v
+--------------------------------+
|  RUN CONTAINER                 |
|  - Different base image possible|
|  - prepareCommands: run BEFORE |
|    deploy files arrive         |
|  - Deploy files land at /var/www|
|  - start: launches the app     |
+--------------------------------+
```

**Phase ordering:**
1. `build.prepareCommands` -- install tools, cached in base layer
2. `build.buildCommands` -- compile, bundle, test
3. `build.deployFiles` -- select artifacts to transfer
4. `run.prepareCommands` -- customize runtime image (runs BEFORE deploy files arrive!)
5. Deploy files arrive at `/var/www`
6. `run.initCommands` -- per-container-start tasks (migrations)
7. `run.start` -- launch the application

**Critical**: `run.prepareCommands` executes BEFORE deploy files are at `/var/www`. Do NOT reference `/var/www/` paths in `run.prepareCommands`. Use `build.addToRunPrepare` to copy files to `/home/zerops/`, then reference `/home/zerops/` in `run.prepareCommands`.

## Networking

```
Internet -> L7 Load Balancer (SSL termination) -> container VXLAN IP:port -> app
```

- **L7 LB terminates SSL/TLS** -- all internal traffic is plain HTTP
- Valid port range: **10-65435** (80/443 reserved by Zerops for SSL termination; exception: PHP uses port 80)
- Cloudflare SSL must be **Full (strict)** -- "Flexible" causes infinite redirect loops

## Storage

- **Container disk**: per-container, persistent, **grow-only** (auto-scaling only increases, never shrinks; to reduce: recreate service)
- **Shared storage**: NFS mount at `/mnt/{hostname}`, POSIX-only, max 60 GB, SeaweedFS backend. Do NOT use for user uploads or frequently-written files -- use Object Storage instead. Shared storage is for cases requiring a shared POSIX filesystem (shared config, plugin directories)
- **Object storage**: S3-compatible (MinIO backend), `forcePathStyle: true` REQUIRED, region `us-east-1`, one auto-named bucket per service (immutable name). Preferred for file uploads, media, and any high-throughput file operations

## Scaling

- **Vertical**: CPU (shared or dedicated), RAM (dual-threshold triggers), Disk (grow-only). Applies to runtimes AND managed services. Does NOT apply to shared-storage or object-storage
- **Horizontal**: 1-10 containers for **runtimes only**. Managed services have fixed container counts: NON_HA=1, HA=3 -- do NOT set minContainers/maxContainers for managed services
- **HA mode** (managed services): fixed 3 containers with master-replica topology, auto-failover. Container count is IMMUTABLE
- **Runtime services are always HA** — the `mode` field on runtimes is accepted but forced to HA regardless of input. Runtime availability is controlled via `minContainers`/`maxContainers` (horizontal scaling), not `mode`
- **Docker**: fixed resources only (no min-max autoscaling), resource change triggers VM restart

## Base Image Contract

| Base | OS | Package Manager | Size | libc |
|------|----|----------------|------|------|
| Alpine (default) | Alpine Linux | `apk add --no-cache` | ~5 MB | musl |
| Ubuntu | Ubuntu | `sudo apt-get update && sudo apt-get install -y` | ~100 MB | glibc |

**NEVER cross them**: `apt-get` on Alpine -> "command not found". `apk` on Ubuntu -> "command not found".

Build containers run as user `zerops` with **sudo** access.

## Container Lifecycle

- **Deploy** = new container. Local files LOST. Only `deployFiles` content survives.
- **Restart, reload, stop/start, vertical scaling** = same container. Local files intact.
- Persistent data: database, object storage, or shared storage. Never local filesystem for anything that must survive a deploy.
- Sessions and cache: use external store (Valkey, database) when running multiple containers.

## Immutable Decisions

These CANNOT be changed after creation — choose correctly or delete+recreate:
- **Hostname** — becomes internal DNS name, max 40 chars, a-z and 0-9 only
- **Mode** (HA/NON_HA) — determines node topology for managed services (1 vs 3 containers). Immutable.
- **Object storage bucket name** — auto-generated from hostname + random prefix
- **Service type category** — cannot change a runtime to a managed service or vice versa

## Platform Constraints

Non-negotiable rules. Violating any causes failures.

- MUST bind `0.0.0.0` (not localhost). L7 LB routes to container VXLAN IP. Binding localhost = 502.
- Internal traffic = plain HTTP. NEVER `https://` between services.
- MUST trust proxy headers. Framework config: see runtime recipe ## Gotchas.
- Deploy = new container. `deployFiles` MANDATORY — without it, run container starts empty.
- Build and Run = SEPARATE containers. `deployFiles` = the ONLY bridge.
- `run.prepareCommands` runs BEFORE deploy files arrive. Never reference `/var/www/` there.
- Zerops injects env vars as OS env vars. Do NOT create `.env` files — empty values shadow OS vars.
- Cross-service wiring: `${hostname_varname}` in zerops.yaml `run.envVariables`.
- import.yaml service level: `envSecrets` ONLY (not `envVariables` — silently dropped by API).
- Shared secrets (APP_KEY, SECRET_KEY_BASE): MUST be project-level, not per-service envSecrets.
- Migrations: `zsc execOnce ${appVersionId} -- <command>` in `initCommands`.
- Sessions: external store (Valkey, database) when running multiple containers.
