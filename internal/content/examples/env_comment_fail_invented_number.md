---
surface: env-comment
verdict: fail
reason: invented-number
title: "2 GB object-storage quota invented across 6 tiers (v38 F-21)"
---

> ```yaml
> # Object storage for user uploads — 2 GB quota suitable for agent iteration
> # and small-production workloads. Promote to 10 GB at HA-prod.
> - hostname: storage
>   type: object-storage
>   objectStorageSize: 1
>   objectStoragePolicy: private
> ```

**Why this fails the env-comment test.**
The yaml emits `objectStorageSize: 1` (1 GB). The comment claims `2 GB
quota`, and the "promote to 10 GB" reference has no matching field.
Pure fabrication — the author wrote a plausible-sounding number without
consulting the yaml they were annotating.

**Correct shape**: describe the TRADE-OFF, not a number.

```yaml
# Object storage for user uploads — sized for agent-iteration workloads;
# expect single-digit GB of combined file uploads. Bump
# objectStorageSize when upload volume makes sustained small-file
# throughput meaningful (typically when switching to HA production
# alongside the mode: HA flip on data services).
- hostname: storage
  type: object-storage
  objectStorageSize: 1
  objectStoragePolicy: private
```

Every number claimed in a comment must be visible in the yaml block
the comment annotates. No exceptions.
