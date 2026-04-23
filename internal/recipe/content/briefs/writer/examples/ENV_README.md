# ENV_README

## Pass

```
# Tier 3 — Stage

**Audience**: reviewer validating the prod build path before release.
**Scale**: single replica, managed services NON_HA.

**From tier 2**: services stop cross-linking to localhost; a
`minFreeRamGB: 0.25` floor appears for OOM protection.

**To tier 4**: production adds `minContainers: 2` for throughput.
Stage data is ephemeral.
```

## Fail — boilerplate, no teaching

```
# Tier 3 — Stage

This is the stage environment.

- db
- app
```

Fails "when do I outgrow": audience/scale/promotion absent. Target is
40-80 lines of teaching.
