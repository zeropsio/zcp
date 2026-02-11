# Meilisearch on Zerops

## Keywords
meilisearch, search, full-text, typo-tolerant, instant search, api key, masterkey, 7700

## TL;DR
Meilisearch on Zerops is single-node only (no clustering), runs on port 7700, and provides three API keys: `masterKey` (admin), `defaultSearchKey` (frontend-safe), `defaultAdminKey` (backend).

## Zerops-Specific Behavior
- Port: **7700**
- Single-node only — no cluster/HA support
- API keys:
  - `masterKey` — Root access (setup/management, never frontend)
  - `defaultSearchKey` — Read-only search (safe for frontend)
  - `defaultAdminKey` — Full admin access (backend only)
- Modes:
  - **Production** (default) — Search Preview disabled, optimized
  - **Development** — Search Preview (mini-dashboard) enabled
- Public HTTPS: Via Zerops subdomain when enabled
- Custom API keys supported for fine-grained access

## Backup
Native dump command: `.dump` format (standard Meilisearch format).

## Configuration
```yaml
# import.yaml
services:
  - hostname: search
    type: meilisearch@1.10
    mode: NON_HA
```

## Connection Pattern
```
http://${hostname}:7700
Authorization: Bearer ${masterKey}
```

## Gotchas
1. **No HA/clustering**: Single-node only — for HA search, use Elasticsearch or Typesense
2. **Never expose `masterKey` to frontend**: Use `defaultSearchKey` for client-side search
3. **Production mode by default**: No search preview dashboard — switch to Development mode for debugging
4. **Single-node data risk**: Use backups — single node means data loss on hardware failure

## See Also
- zerops://decisions/choose-search
- zerops://services/elasticsearch
- zerops://services/typesense
- zerops://platform/backup
