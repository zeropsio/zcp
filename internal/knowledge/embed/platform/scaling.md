# Scaling on Zerops

## Keywords
scaling, autoscaling, vertical, horizontal, cpu, ram, disk, containers, replicas, scale up, scale down, resources, minFreeRamGB, minFreeRamPercent, free ram, ram threshold, startCpuCoreCount, minFreeCpuCores, minFreeCpuPercent

## TL;DR
Zerops auto-scales both vertically (CPU/RAM/disk) and horizontally (1-10 containers), with configurable min/max ranges and automatic threshold detection.

## Vertical Scaling (Resources)

Applies to: runtimes, databases, shared storage, Linux containers (Alpine/Ubuntu).

### CPU
- **Shared CPU**: Physical core shared with up to 10 apps (1/10 to 10/10 power)
- **Dedicated CPU**: Exclusive physical core access
- CPU mode change: Once per hour
- Scale-up window: 20 seconds | Scale-down window: 60 seconds
- Min step: 0.1 cores | Max step: 40 cores
- Default min free CPU: 10%
- Threshold: 60th percentile → scale up, 40th percentile → scale down
- `startCpuCoreCount` — number of CPU cores at container start (default 2)
- `minFreeCpuCores` — absolute threshold of free CPU cores to maintain
- `minFreeCpuPercent` — dynamic percentage threshold of free CPU (default 0% = disabled)

### RAM
- Scale-up window: 10 seconds | Scale-down window: 120 seconds
- Min step: 0.125 GB | Max step: 32 GB
- Default min free RAM: 0.0625 GB (64 MB)
- **Dual-threshold mechanism** — RAM scales based on the **higher** of two thresholds:
  - `minFreeRamGB` — absolute minimum free RAM (e.g., 0.5 means at least 0.5 GB must be free)
  - `minFreeRamPercent` — percentage of granted RAM that must stay free (e.g., 50 with 4 GB granted = 2 GB free)
  - System evaluates both and uses **whichever threshold is higher** to decide when to scale

### Disk
- Scale-up window: 10 seconds | Scale-down window: 300 seconds
- Min step: 0.5 GB | Max step: 128 GB
- **Disk can only grow** — never shrinks automatically

### Docker Exception
Docker services use **fixed resources** (no min-max ranges). Changing resources triggers VM restart.

## Horizontal Scaling (Containers)

- Min: 1 container | Max: 10 containers
- Applies to: runtimes, Linux containers, Docker (VMs)
- Databases & shared storage: **Fixed container count** (set at creation, HA = 3 nodes)
- App must be stateless/HA-ready for horizontal scaling

## HA Mode
- **Immutable after creation** — cannot switch between HA/NON_HA
- HA recovery: Failed container disconnected → new created → data synced → old removed
- Single container mode: No automatic recovery, data loss risk

## Configuration (import.yml)
```yaml
services:
  - hostname: api
    type: nodejs@22
    minContainers: 1
    maxContainers: 3
    verticalAutoscaling:
      cpuMode: SHARED               # SHARED or DEDICATED
      minCpu: 1
      maxCpu: 5
      startCpuCoreCount: 2          # CPU cores at container start
      minFreeCpuCores: 0.5          # absolute free CPU threshold
      minFreeCpuPercent: 20         # % free CPU threshold (0 = disabled)
      minRam: 0.5
      maxRam: 4
      minFreeRamGB: 0.5             # absolute free RAM threshold (GB)
      minFreeRamPercent: 50         # % of granted RAM that must stay free
      minDisk: 1
      maxDisk: 20
```

## Gotchas
1. **CPU mode change limit**: Can only change shared↔dedicated once per hour
2. **Disk never shrinks**: Auto-scaling only increases disk — to reduce, recreate the service
3. **Docker scaling restarts VM**: Vertical scaling on Docker causes downtime
4. **Database horizontal scaling is fixed**: Set at creation, cannot change later

## See Also
- zerops://platform/infrastructure
- zerops://config/import-yml
- zerops://gotchas/common
