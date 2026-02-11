# Elasticsearch on Zerops

## Keywords
elasticsearch, es, search, full-text, elastic, cluster, plugin, heap, 9200, elk

## TL;DR
Elasticsearch on Zerops uses port 9200 (HTTP only), authenticates with `elastic` user, and manages plugins via the `PLUGINS` env var (comma-separated, auto-installed on restart).

## Zerops-Specific Behavior
- Port: **9200** (HTTP only, no native transport port exposed)
- Default user: `elastic` (auto-created, password in Access Details)
- Plugin management: `PLUGINS` env var (comma-separated list)
- JVM heap: `HEAP_PERCENT` env var (default 50% of container RAM, range 1-100)
- Min RAM: 0.25 GB
- Cluster support: Multiple nodes in HA mode

## Plugin Management
```yaml
envVariables:
  PLUGINS: analysis-icu,ingest-attachment
```
- Auto-installed at startup
- Removing a plugin from the list triggers uninstallation on restart
- Changes require service restart

## Backup
Format: `elasticdump` (`.gz` per index/component, gzip-compressed JSON).

## Configuration
```yaml
# import.yaml
services:
  - hostname: search
    type: elasticsearch@8.16
    mode: HA
```

## Connection Pattern
```
http://${hostname}:9200
Authorization: Basic elastic:<password>
```

## Gotchas
1. **HTTP only**: No HTTPS internally — SSL terminates at L7 balancer
2. **Plugins need restart**: Changing `PLUGINS` env var requires service restart to take effect
3. **`HEAP_PERCENT` needs restart**: JVM heap changes require restart
4. **50% default heap**: May need tuning — set `HEAP_PERCENT=75` for search-heavy workloads

## See Also
- zerops://decisions/choose-search
- zerops://services/meilisearch
- zerops://operations/metrics
- zerops://platform/backup
