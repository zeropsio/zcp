# Networking Overview on Zerops

## Keywords
networking, internal, vxlan, private network, service discovery, hostname, http, internal access, service communication

## TL;DR
Each Zerops project gets an isolated VXLAN private network where services communicate via `http://hostname:port` — never use HTTPS for internal communication.

## Internal Communication
- Each project has an isolated private VXLAN network
- Services discover each other by hostname: `http://hostname:port`
- **Never use HTTPS internally** — SSL terminates at the L7 balancer
- No cross-project internal communication

## Service Discovery
- Hostname = service name (set at creation, immutable)
- Internal ports defined in `zerops.yaml` (HTTP, TCP, or UDP)
- Auto-generated env vars: `hostname`, `port`, `connectionString`

## Cross-Service Variable Reference
```yaml
envVariables:
  DB_URL: postgresql://${db_user}:${db_password}@${db_hostname}:5432/${db_dbname}
```

## L7 Balancer
- 4000 worker connections
- 30s keepalive timeout
- Max upload: 512MB (public), 50MB (zerops.app subdomain)
- Round-robin load balancing with health checks
- 2 HA containers per project (custom domain access)

## Port Ranges
- Internal ports: 10-65435
- Ports 80, 443: Reserved by Zerops for SSL termination
- Define in `zerops.yaml` under `run.ports`

## Gotchas
1. **Never use HTTPS internally**: SSL terminates at the L7 balancer — internal traffic is plain HTTP
2. **Never listen on port 443**: Zerops handles SSL — your app should listen on a standard HTTP port (3000, 8080, etc.)
3. **Hostname is immutable**: Cannot change after service creation
4. **No cross-project communication**: Services in different projects cannot reach each other internally

## See Also
- zerops://networking/public-access
- zerops://networking/firewall
- zerops://networking/vpn
- zerops://platform/infrastructure
