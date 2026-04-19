---
id: bootstrap-adopt-discover
priority: 2
phases: [bootstrap-active]
routes: [adopt]
steps: [discover]
title: "Adopt — discover existing services"
---

### Adopting existing services

The project already has non-managed services that predate ZCP's view of
infrastructure. Adopt does NOT touch their code, configuration, or scale —
it only writes a `ServiceMeta` per service so downstream workflows can
target them.

Start by listing what's there:

```
zerops_discover
```

Read every user (non-system, non-managed) service in the output. For each
one, note:

- the hostname (keep verbatim; do not rename)
- the runtime type (`ServiceStackTypeVersionName`)
- whether ports are exposed (dynamic/implicit-web vs static)

This inventory feeds the next step — prompting the user for mode and
strategy per service.
