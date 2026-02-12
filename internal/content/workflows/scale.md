# Scale: Scaling Zerops Services

## Overview

Scale services horizontally (more containers) and vertically (more CPU/RAM/disk).

## Steps

### 1. Check Current Scaling

Inspect the service to see current resource configuration:

```
zerops_discover service="api"
```

### 2. Vertical Scaling (CPU/RAM/Disk)

Adjust resource limits per container:

```
zerops_scale serviceHostname="api" minCpu=1 maxCpu=4 minRam=0.5 maxRam=4
```

Parameters:
- `cpuMode` — SHARED (burstable, cheaper) or DEDICATED (guaranteed).
- `minCpu` / `maxCpu` — CPU core range for autoscaling.
- `minRam` / `maxRam` — RAM in GB range for autoscaling.
- `minDisk` / `maxDisk` — Disk in GB range for autoscaling.

### 3. Horizontal Scaling (Containers)

Adjust container count range:

```
zerops_scale serviceHostname="api" minContainers=2 maxContainers=5
```

Parameters:
- `minContainers` — minimum container count (always running).
- `maxContainers` — maximum container count (autoscale ceiling).

### 4. Track Scaling Operation

```
zerops_process processId="<id from scale>"
```

## Scaling Rules

- `min` values must be less than or equal to `max` values.
- At least one scaling parameter must be provided.
- CPU mode options: `SHARED` (default, burstable) or `DEDICATED` (guaranteed cores).
- HA mode is set at service creation and cannot be changed via scaling.

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

- Zerops autoscales within your min/max range automatically -- no manual intervention needed.
- SHARED CPU is fine for most workloads. Use DEDICATED only for latency-sensitive services.
- Horizontal scaling (more containers) is generally better than vertical for stateless services.
- Database services have different scaling constraints -- check Zerops docs via `zerops_knowledge`.
