---
id: bootstrap-adopt-discover
priority: 2
phases: [bootstrap-active]
routes: [adopt]
steps: [discover]
title: "Adopt — discover existing services"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.ServiceSnapshot.Mode, workflow.ServiceSnapshot.CloseDeployMode, ops.ServiceInfo.AdoptionState]
---

### Adopting existing services

Adoption attaches ZCP tracking to an existing runtime service without touching its code, configuration, or scale. After adopt close, the envelope reports each adopted hostname with `bootstrapped: true` and an empty close-mode / git-push capability — populated later when the develop session needs them.

List what's there:

```
zerops_discover
```

Use services where `adoptionState="adoptable"` from the discover output — the per-service field already filters out managed deps (`adoptionState="managed-dep"`), the ZCP control-plane container (`"zcp-self"`), already-adopted runtimes (`"adopted"`), and mid-bootstrap services owned by a prior session (`"resumable"` — those route through `resume`, not `adopt`). For each adoptable hostname, note:

- the hostname (keep verbatim; do not rename)
- the runtime type (`ServiceStackTypeVersionName`)
- whether ports are exposed (dynamic/implicit-web vs static)
