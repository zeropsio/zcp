---
id: bootstrap-adopt-discover
priority: 2
phases: [bootstrap-active]
routes: [adopt]
steps: [discover]
title: "Adopt — discover existing services"
---

### Adopting existing services

Adopt writes `ServiceMeta` per service. It does NOT touch code,
configuration, or scale.

List what's there:

```
zerops_discover
```

Read every user (non-system, non-managed) service. For each, note:

- the hostname (keep verbatim; do not rename)
- the runtime type (`ServiceStackTypeVersionName`)
- whether ports are exposed (dynamic/implicit-web vs static)
