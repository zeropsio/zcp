# Zerops Platform Model

## Keywords
zerops, platform, architecture, lifecycle, build, deploy, run, container, networking, vxlan, scaling, storage, state, immutable, hostname, mode, base image, alpine, ubuntu, mental model

## TL;DR
Conceptual model of how Zerops works — lifecycle, networking, storage, scaling, state. Understanding these mechanics enables correct YAML generation for ANY scenario.

---

## 1. Container Universe

Everything on Zerops runs in **full Linux containers** (Incus, not Docker). Each container has:
- Full SSH access, working directory `/var/www`
- Connected via VXLAN private network (per project)
- Addressable by service hostname (internal DNS)
- Own disk (persistent, grow-only)

Hierarchy: **Project → Service → Container(s)**. One project = one isolated network. Services communicate by hostname over this network.

## 2. The Two YAML Files

| File | Purpose | Scope |
|------|---------|-------|
| `import.yml` | **Topology** — WHAT exists | Services, types, versions, scaling, env vars |
| `zerops.yml` | **Lifecycle** — HOW it runs | Build, deploy, run commands per service |

These are separate concerns. `import.yml` creates infrastructure. `zerops.yml` defines what happens when code is pushed. A service can exist (imported) without any code deployed yet.

## 3. The Build/Deploy Lifecycle

This is the most critical concept. Build and Run are **SEPARATE containers** with **separate base images**.

```
Source Code
    ↓
┌─────────────────────────────────┐
│  BUILD CONTAINER                │
│  - Starts with base image ONLY  │
│  - No pre-installed build tools  │
│    (no Maven, no Gradle, etc.)   │
│  - prepareCommands: cached setup │
│  - buildCommands: compile/bundle │
│  - Output: deployFiles           │
└──────────┬──────────────────────┘
           │ deployFiles = THE ONLY BRIDGE
           ↓
┌─────────────────────────────────┐
│  RUN CONTAINER                  │
│  - Different base image possible │
│  - prepareCommands: run BEFORE   │
│    deploy files arrive           │
│  - Deploy files land at /var/www │
│  - start: launches the app       │
└─────────────────────────────────┘
```

**Key consequences:**
- Build container has ONLY the base runtime (e.g., `java@21` = JDK + git + wget). Maven, Gradle, pip, etc. are NOT pre-installed — you must install them via `prepareCommands` or use wrappers (`./mvnw`)
- `deployFiles` is the ONLY way to transfer artifacts from build to run. Nothing else survives
- For Java: this means fat/uber JARs are mandatory (all dependencies must be in the JAR)
- For Node.js: `node_modules` must be in `deployFiles` (runtime doesn't run `npm install`)
- For Python: use `addToRunPrepare` + `run.prepareCommands` for pip packages (see runtime docs)

**Phase ordering:**
1. `build.prepareCommands` — install tools, cached in base layer
2. `build.buildCommands` — compile, bundle, test
3. `build.deployFiles` — select artifacts to transfer
4. `run.prepareCommands` — customize runtime image (runs BEFORE deploy files arrive!)
5. Deploy files arrive at `/var/www`
6. `run.initCommands` — per-container-start tasks (migrations)
7. `run.start` — launch the application

**Critical**: `run.prepareCommands` executes BEFORE deploy files are at `/var/www`. Do NOT reference `/var/www/` paths in `run.prepareCommands`. Use `build.addToRunPrepare` to copy files to `/home/zerops/`, then reference `/home/zerops/` in `run.prepareCommands`.

## 4. Networking Model

```
Internet → L7 Load Balancer (SSL termination) → container VXLAN IP:port → app
```

- **L7 LB terminates SSL/TLS** — all internal traffic is plain HTTP
- LB connects to the container's **VXLAN IP**, NOT localhost
- Apps **MUST bind `0.0.0.0`** — binding localhost/127.0.0.1 makes the app unreachable from the LB → **502 Bad Gateway**
- Internal service-to-service: always `http://hostname:port` — NEVER `https://`
- Valid port range: **10-65435** (80/443 reserved by Zerops for SSL termination; exception: PHP uses port 80)

## 5. Storage Model

- **Container disk**: per-container, persistent, **grow-only** (auto-scaling only increases, never shrinks; to reduce: recreate service)
- **Shared storage**: NFS mount at `/mnt/{hostname}`, POSIX-only, max 60 GB, SeaweedFS backend
- **Object storage**: S3-compatible (MinIO backend), `forcePathStyle: true` REQUIRED, region `us-east-1`, runs on independent infrastructure, one auto-named bucket per service (immutable name)

## 6. Scaling Model

- **Vertical** (more resources): CPU (shared or dedicated), RAM (dual-threshold triggers), Disk (grow-only). Applies to runtimes AND managed services (DB, cache). Does NOT apply to shared-storage or object-storage
- **Horizontal** (more containers): 1-10 containers for **runtimes only**. Managed services have fixed container counts: NON_HA=1, HA=3 — do NOT set minContainers/maxContainers for managed services
- **HA mode**: fixed 3 containers with master-replica topology, auto-failover. Container count is IMMUTABLE for managed services
- **Docker**: fixed resources only (no min-max autoscaling), resource change triggers VM restart

## 7. Service State Model

Services have states that determine allowed operations:
- `READY_TO_DEPLOY` — runtime service created but no code deployed yet. Cannot enable subdomain (use `enableSubdomainAccess: true` in import.yml instead)
- `RUNNING` / `ACTIVE` — after successful deploy. Full operations available
- Managed services (DB, cache) start `ACTIVE` immediately (no deploy needed)

## 8. Immutable Decisions

These CANNOT be changed after creation — choose correctly or delete+recreate:
- **Hostname** — becomes internal DNS name, max 25 chars, a-z and 0-9 only
- **Mode** (HA/NON_HA) — determines node topology (1 vs 3 containers)
- **Object storage bucket name** — auto-generated from hostname + random prefix
- **Service type category** — cannot change a runtime to a managed service or vice versa

## 9. Base Image Contract

The base image COMPLETELY determines the OS, package manager, and available tools:

| Base | OS | Package Manager | Size | libc |
|------|----|----------------|------|------|
| Alpine (default) | Alpine Linux | `apk add --no-cache` | ~5 MB | musl |
| Ubuntu | Ubuntu | `sudo apt-get update && sudo apt-get install -y` | ~100 MB | glibc |

**NEVER cross them**: `apt-get` on Alpine → "command not found". `apk` on Ubuntu → "command not found".

Build containers run as user `zerops` with **sudo** access. Each runtime base includes specific pre-installed tools (see runtime docs).

## 10. Causal Chains

Understanding cause→effect prevents debugging by trial-and-error:

| Action | Effect | Root Cause |
|--------|--------|------------|
| Bind localhost | 502 Bad Gateway | LB routes to VXLAN IP, not localhost |
| Deploy thin JAR | ClassNotFoundException | Build≠Run containers; dependencies not in artifact |
| `apt-get` on Alpine | "command not found" | Alpine uses `apk`, not `apt-get` |
| Reference `/var/www` in `run.prepareCommands` | File not found | Deploy files arrive AFTER prepareCommands |
| `enableSubdomainAccess` in import + call `zerops_subdomain` | Error | Service in READY_TO_DEPLOY state rejects subdomain API call |
| `npm install` only in build | Missing modules at runtime | Build container discarded; `node_modules` must be in `deployFiles` |
| Bare `mvn` in buildCommands | "command not found" | Build image has JDK only; Maven not pre-installed |
| `valkey@8` in import | Import fails | Only `valkey@7.2` is valid (8 passes dry-run but fails real import) |
| No `mode` for managed service | Import fails | Managed services (DB, cache) require explicit `mode: NON_HA` or `mode: HA` |
| Set `minContainers` for PostgreSQL | Import fails | Managed services have fixed container counts |

## See Also
- zerops://foundation/rules — actionable DO/DON'T rules with reasons
- zerops://foundation/grammar — YAML schema reference
- zerops://foundation/runtimes — runtime-specific deltas
- zerops://foundation/services — managed service reference
