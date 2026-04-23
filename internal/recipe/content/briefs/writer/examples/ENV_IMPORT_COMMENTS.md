# ENV_IMPORT_COMMENTS

## Pass — each block explains its own decision

```yaml
# Tier 5 HA: two replicas behind the L7 balancer. minContainers: 2 gates
# the rolling-deploy contract; cpuMode: DEDICATED removes shared-CPU
# contention that's tolerable on stage but not prod.
- hostname: app
  mode: HA
  minContainers: 2

# Postgres HA: managed failover. ~2x stage spend buys ~3s failover —
# the floor for customer-facing tiers.
- hostname: db
  mode: HA
```

## Fail — templated phrase copy-pasted

```yaml
# This service enables zero-downtime rolling deploys.
- hostname: app
# This service enables zero-downtime rolling deploys.
- hostname: db
```

Fails "explain a decision": every comment is the same generic phrase.
