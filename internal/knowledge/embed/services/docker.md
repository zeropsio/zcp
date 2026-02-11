# Docker on Zerops

## Keywords
docker, vm, virtual machine, container image, docker compose, network host, pre-built image, docker run

## TL;DR
Docker services run in VMs (not containers), require `--network=host`, have fixed resources (no auto-scaling ranges), and VM restarts on any resource change.

## Zerops-Specific Behavior
- **Runs in a VM**, not a container — slower boot, higher resource overhead
- Build phase runs in containers (fast), but runtime is VM-based
- Network: **Must use `--network=host`** (or `network_mode: host` in compose)
- Resources: Fixed values only (no min/max auto-scaling ranges)
- Resource change triggers VM restart (brief downtime)
- Disk: Can only increase — never decrease without recreation

## Configuration
```yaml
# zerops.yaml
zerops:
  - setup: myservice
    build:
      base: alpine@3.20
      prepareCommands:
        - docker pull myregistry/myapp:1.2.3
      buildCommands:
        - docker save myregistry/myapp:1.2.3 -o image.tar
      deployFiles: ./image.tar
    run:
      start: docker load -i image.tar && docker run --network=host myregistry/myapp:1.2.3
```

```yaml
# Docker Compose example
version: "3"
services:
  app:
    image: myapp:1.2.3
    network_mode: host
```

## Gotchas
1. **Always use `--network=host`**: Without it, the container cannot receive traffic from Zerops routing
2. **Never use `:latest` tag**: Zerops caches images — `:latest` won't be re-pulled, use specific version tags
3. **VM restart on resource change**: Vertical scaling causes downtime — plan resource changes during maintenance windows
4. **No auto-scaling ranges**: Docker services use fixed CPU/RAM values, not min/max ranges
5. **Disk only grows**: Cannot decrease disk size — must recreate the service
6. **Build phase is container-based**: `prepareCommands` and `buildCommands` run in containers, not the VM

## See Also
- zerops://decisions/choose-runtime-base
- zerops://platform/scaling
- zerops://gotchas/common
