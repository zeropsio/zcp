# Docker on Zerops

## Keywords
docker, dockerfile, compose, vm, container, network host, zerops.yml

## TL;DR
Docker runs in a VM (not container) -- slower boot, higher overhead. `--network=host` is MANDATORY. No autoscaling -- resource changes require VM restart.

### VM Runtime

Runs in **VM** (not container) -- slower boot, higher overhead.

### Networking

`--network=host` MANDATORY (or `network_mode: host` in compose).

### Resources

Fixed values only (no min-max autoscaling). Resource change triggers VM restart.

### Image Tags

Never use `:latest` tag -- cached, won't re-pull.
