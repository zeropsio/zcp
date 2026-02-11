# NATS on Zerops

## Keywords
nats, message queue, jetstream, pub-sub, lightweight messaging, nats server, stream, queue group

## TL;DR
NATS on Zerops has JetStream enabled by default (40GB memory + 250GB file store), requires restart for config changes, and uses port 4222 for clients.

## Zerops-Specific Behavior
- Ports:
  - **4222** — Client connections
  - **8222** — HTTP monitoring interface
- Auth: user `zerops` + auto-generated 16-char password
- Connection: `nats://${user}:${password}@${hostname}:4222`
- JetStream: Enabled by default (`JET_STREAM_ENABLED=1`, set `0` to disable)
- Max message: `MAX_PAYLOAD` — default 8MB, max 64MB
- Health check: `GET /healthz` on port 8222
- **Config changes require service restart** (no hot-reload)

## JetStream Storage
- Memory store: Up to 40GB (high-performance caching)
- File store: Up to 250GB (persistent storage)
- HA sync interval: 1 minute across nodes

## HA Mode
- Multi-node NATS cluster
- Automatic route configuration between nodes
- Improved reliability vs NON_HA

## NON_HA Mode
- Data persistence not guaranteed on failures
- Lower resource requirements

## Configuration
```yaml
# import.yaml
services:
  - hostname: messaging
    type: nats@2.10
    mode: HA
```

## Backup
Filesystem archival: `.tar.gz` format of queue state.

## Gotchas
1. **Restart required for config changes**: Changing env vars like `MAX_PAYLOAD` needs service restart — no hot-reload
2. **JetStream HA sync lag**: 1-minute sync interval — brief data lag possible between nodes
3. **8MB default payload**: Increase `MAX_PAYLOAD` if sending large messages (max 64MB)
4. **JetStream is on by default**: Set `JET_STREAM_ENABLED=0` if you only need core NATS pub-sub

## See Also
- zerops://decisions/choose-queue
- zerops://services/kafka
- zerops://platform/backup
