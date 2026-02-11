# Shared Storage on Zerops

## Keywords
shared storage, filesystem, seaweedfs, mount, posix, shared files, persistent storage, nfs, file system

## TL;DR
Shared Storage on Zerops uses SeaweedFS with POSIX mount at `/mnt/{hostname}`, supports HA with 1:1 replication, and has a max capacity of 60GB.

## Zerops-Specific Behavior
- Backend: SeaweedFS
- Mount point: `/mnt/{hostname}` (POSIX filesystem)
- Max capacity: 60 GB
- HA: 1:1 replication across nodes (data on both)
- NON_HA: Single node — data loss on failure
- Accessible from any service in the same project

## HA Mode
- Data replicated 1:1 across nodes
- Both nodes have full copy
- Automatic failover

## NON_HA Mode
- Single node
- Data loss on hardware failure
- Use for non-critical or reproducible data only

## Configuration
```yaml
# import.yaml
services:
  - hostname: files
    type: shared-storage
    mode: HA
```

### Using in Runtime Service
```yaml
# zerops.yaml
zerops:
  - setup: myapp
    run:
      start: node app.js
      mount:
        - files  # hostname of shared storage service
```

Files accessible at `/mnt/files/` from the runtime service.

## Gotchas
1. **60GB max**: Cannot exceed 60GB — use Object Storage for larger files
2. **POSIX only**: Not S3-compatible — for S3 API, use Object Storage
3. **Mount by hostname**: Must reference the shared storage service hostname in `zerops.yaml` mount section
4. **HA is immutable**: Cannot switch HA/NON_HA after creation

## See Also
- zerops://services/object-storage
- zerops://config/zerops-yml
- zerops://platform/backup
