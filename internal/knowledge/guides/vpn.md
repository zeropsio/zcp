# VPN on Zerops

## Keywords
vpn, wireguard, zcli vpn, vpn up, vpn down, local development, service access, mtu

## TL;DR
Zerops VPN uses WireGuard via `zcli vpn up <project-id>` — connects to one project at a time, services accessible by hostname, but env vars are NOT available through VPN.

## Commands
```bash
zcli vpn up <project-id>                    # Connect
zcli vpn up <project-id> --auto-disconnect  # Auto-disconnect on terminal close
zcli vpn up <project-id> --mtu 1350        # Custom MTU (default 1420)
zcli vpn down                               # Disconnect
```

## Behavior
- All services accessible via hostname (e.g., `db.zerops`, `api.zerops`)
- **One project at a time** — connecting to another disconnects the current
- Automatic reconnection with daemon
- **Environment variables NOT available** through VPN — use GUI or API to read them

## Hostname Resolution
- Append `.zerops` to hostname: `hostname.zerops`
- Example: `postgresql://user:pass@db.zerops:5432/mydb`

## Troubleshooting

| Problem | Solution |
|---------|----------|
| Interface already exists | `zcli vpn down` then `zcli vpn up` |
| Hostname not resolving | Append `.zerops` (e.g., `db.zerops`) |
| WSL2 not working | Enable systemd in `/etc/wsl.conf` under `[boot]` |
| Conflicting VPN | Use `--mtu 1350` |
| Ubuntu 25.* issues | Install AppArmor utilities |

## Gotchas
1. **No env vars via VPN**: Must read env vars from GUI or API — VPN only provides network access
2. **One project at a time**: Cannot connect to multiple projects simultaneously
3. **Hostname needs `.zerops` suffix**: Plain hostname may not resolve — always use `hostname.zerops`

## See Also
- zerops://guides/networking
- zerops://guides/firewall
