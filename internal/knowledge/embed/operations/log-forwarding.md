# Log Forwarding on Zerops

## Keywords
log forwarding, better stack, papertrail, elk, logstash, syslog-ng, external logging, log aggregation

## TL;DR
Zerops supports log forwarding to Better Stack, Papertrail, or self-hosted ELK via syslog — use syslog-ng with source name `s_src` (not `s_sys`).

## Ready-Made Integrations
- **Better Stack** — cloud log management
- **Papertrail** — cloud log aggregation
- **ELK Stack** — self-hosted (Elasticsearch + Logstash + Kibana)

## ELK Stack Setup (Self-Hosted on Zerops)

Services needed:
- `elkstorage` — Elasticsearch
- `kibana` — UI
- `logstash` — Log collection (UDP syslog)

### Multi-Project Forwarding
Make Logstash public with firewall whitelist rules. Forward logs from other projects to the Logstash endpoint.

## Custom syslog-ng Configuration

**Critical**: Use source name `s_src` (not `s_sys`):
```
source s_src {
    system();
    internal();
};
```

### Certificate paths
- System certs: `/etc/ssl/certs`
- Custom certs: `ca-file("/etc/syslog-ng/user.crt")`

## Gotchas
1. **Source name must be `s_src`**: Using `s_sys` (common default) will not capture Zerops logs
2. **UDP for Logstash**: Zerops forwards logs via UDP syslog — ensure Logstash listens on UDP
3. **Custom certs path**: Place custom CA certs in `/etc/syslog-ng/user.crt`

## See Also
- zerops://operations/logging
- zerops://operations/metrics
- zerops://services/elasticsearch
