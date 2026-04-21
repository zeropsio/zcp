# Provision — 3. Mount dev filesystem

Mount every dev service's filesystem for direct SSHFS access from the main agent:

```
zerops_mount action="mount" serviceHostname="{devHostname}"
```

Each mount exposes `/var/www/{devHostname}/` on the zcp side as an SSHFS write surface into the container's own `/var/www/` path. All code writes targeting that codebase go through this mount; SSH into the same hostname targets the live container directly.

## One mount per codebase

The number of mounts equals the number of codebases the plan declares, which is a function of `sharesCodebaseWith`:

- Single-runtime plans with no separate worker → one dev mount.
- Single-runtime plans with a separate-codebase worker (`sharesCodebaseWith` empty on the worker) → two dev mounts.
- Dual-runtime plans with a shared-codebase worker (`sharesCodebaseWith` points at the host) → two dev mounts (api + frontend).
- Dual-runtime plans with a separate-codebase worker → three dev mounts (api + frontend + worker).

Iterate over the dev mounts the plan actually produced — one `zerops_mount` call per dev hostname. The authoritative enumeration of zerops.yaml setups per codebase lives under the generate step's "Write ALL setups at once" section; the mount count here matches it one-to-one.
