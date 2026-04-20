---
id: bootstrap-provision-managed-conventions
priority: 2
phases: [bootstrap-active]
routes: [classic, adopt]
steps: [provision]
title: "Managed service hostname conventions"
---

### Managed service hostname conventions

Canonical hostnames (agents/recipes/cross-service refs assume these):

- `db` — postgresql / mariadb / mysql / mongodb
- `cache` — valkey / keydb / redis
- `queue` — nats / kafka / rabbitmq
- `search` — elasticsearch / meilisearch / typesense
- `storage` — object-storage / shared-storage

**Mode for managed services**: Managed services default to `mode: NON_HA`
when the field is omitted. Set `mode: HA` explicitly only for production
deployments where the user has asked for high availability.

**Priority**: Managed services use `priority: 10` so they initialize
before runtime services. Runtime services default (priority 5) or unset.
Databases must be ready before apps that depend on them.
