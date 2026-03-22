# Scale: Scaling Zerops Services

## Overview

Scale services horizontally (more containers) and vertically (more CPU/RAM/disk). `zerops_scale` blocks until the scaling process completes.

**When to scale which way:**

| Symptom | Scale type | Why |
|---------|-----------|-----|
| CPU/memory pressure on existing containers | Vertical (CPU/RAM) | More resources per container |
| High request volume, stateless service | Horizontal (containers) | Distribute load across more instances |
| Disk filling up | Vertical (disk) | More storage per container |
| Latency-sensitive workload on SHARED CPU | CPU mode → DEDICATED | Guaranteed cores, no burstable throttling |

---

## Steps

### Step 1 — Check Current Scaling

Inspect the service to see current resource configuration:

```
zerops_discover service="api"
```

Note: current container count, CPU mode, and resource ranges. These are the live autoscaling values.

### Step 2 — Apply Scaling Changes

#### Vertical Scaling (CPU/RAM/Disk)

Adjust resource limits per container:

```
zerops_scale serviceHostname="api" minCpu=1 maxCpu=4 minRam=0.5 maxRam=4
```

Parameters:
- `cpuMode` — SHARED (burstable, cheaper) or DEDICATED (guaranteed).
- `minCpu` / `maxCpu` — CPU core range for autoscaling.
- `minRam` / `maxRam` — RAM in GB range for autoscaling.
- `minDisk` / `maxDisk` — Disk in GB range for autoscaling.
- `startCpu` — Initial CPU cores when a new container starts (optional, within min/max range).

#### Horizontal Scaling (Containers)

Adjust container count range:

```
zerops_scale serviceHostname="api" minContainers=2 maxContainers=5
```

Parameters:
- `minContainers` — Minimum container count (always running).
- `maxContainers` — Maximum container count (autoscale ceiling).

#### Scaling Thresholds (fine-tuning)

Fine-tune when autoscaling triggers by adjusting free-resource thresholds:

```
zerops_scale serviceHostname="api" minFreeRamGB=0.5 minFreeRamPercent=20
```

Parameters:
- `minFreeRamGB` — Absolute free RAM threshold in GB. Scale-up triggers when free RAM drops below this. Default: 0.0625 (64 MB). Prevents OOM and preserves kernel disk cache.
- `minFreeRamPercent` — Free RAM threshold as percentage of granted RAM (0-100). Scales proportionally with granted RAM. Default: 0 (disabled).
- `minFreeCpuCores` — Free CPU threshold as fraction of one core (0.0-1.0). DEDICATED CPU mode only. Default: 0.1 (10%).
- `minFreeCpuPercent` — Free CPU threshold as percentage of total capacity across all cores (0-100). DEDICATED CPU mode only. Default: 0 (disabled).

Whichever RAM threshold (absolute or percentage) provides **more free memory** wins.

**Production hardening** — combine resource bounds with thresholds:

```
zerops_scale serviceHostname="api" cpuMode="DEDICATED" minCpu=2 maxCpu=8 minRam=2 maxRam=16 minFreeRamGB=0.5 minFreeRamPercent=5 minFreeCpuCores=0.2 minContainers=2 maxContainers=6
```

### Step 3 — Verify New Values

`zerops_scale` blocks until the process completes — returns final status (FINISHED/FAILED). If FAILED, check the error message — common causes:
- `min` value exceeds `max` value
- Requested resources exceed project limits
- Invalid CPU mode value (must be SHARED or DEDICATED)

Confirm the scaling was applied:

```
zerops_discover service="api"
```

Check that the returned scaling values match what you requested.

---

## Scaling Rules

- `min` values must be less than or equal to `max` values.
- `start` values must be within the min/max range.
- At least one scaling parameter must be provided.
- CPU mode options: `SHARED` (default, burstable) or `DEDICATED` (guaranteed cores).
- HA mode is set at service creation and cannot be changed via scaling.
- Managed services (databases, caches) have different scaling constraints — use `zerops_knowledge services=["postgresql@16"]` to check.

## Scaling Strategies

**Development**: SHARED CPU, min resources, 1 container. Cost-effective for dev/staging.

```
zerops_scale serviceHostname="api" cpuMode="SHARED" minCpu=1 maxCpu=2 minRam=0.25 maxRam=1 minContainers=1 maxContainers=1
```

**Production**: DEDICATED CPU, higher minimums, multiple containers for HA.

```
zerops_scale serviceHostname="api" cpuMode="DEDICATED" minCpu=2 maxCpu=8 minRam=2 maxRam=8 minContainers=2 maxContainers=6
```

**Burst workloads**: Wide autoscaling range, SHARED CPU.

```
zerops_scale serviceHostname="worker" cpuMode="SHARED" minCpu=1 maxCpu=8 minRam=1 maxRam=16 minContainers=1 maxContainers=10
```

## Tips

- Zerops autoscales within your min/max range automatically — no manual intervention needed.
- SHARED CPU is fine for most workloads. Use DEDICATED only for latency-sensitive services.
- Horizontal scaling (more containers) is generally better than vertical for stateless services.
- Database services have different scaling constraints. For managed service specifics, use:
  ```
  zerops_knowledge services=["postgresql@16"]
  ```
  This returns the service card with HA behavior, mode requirements, and scaling constraints.

---

## After Scaling

- **Performance still bad?** → `zerops_workflow action="start" workflow="debug"` to diagnose
- **Need to deploy changes?** → `zerops_workflow action="start" workflow="deploy"`
- **Need to change env vars?** → `zerops_workflow action="start" workflow="configure"`
