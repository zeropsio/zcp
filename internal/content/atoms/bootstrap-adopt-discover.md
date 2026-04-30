---
id: bootstrap-adopt-discover
priority: 2
phases: [bootstrap-active]
routes: [adopt]
steps: [discover]
title: "Adopt — discover existing services"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.ServiceSnapshot.Mode, workflow.ServiceSnapshot.CloseDeployMode]
---

### Adopting existing services

Adoption attaches ZCP tracking to an existing runtime service without touching its code, configuration, or scale. After adopt close, the envelope reports each adopted hostname with `bootstrapped: true`; close-mode + git-push capability are left empty (develop configures them on first use).

List what's there:

```
zerops_discover
```

Read every user (non-system, non-managed) service. For each, note:

- the hostname (keep verbatim; do not rename)
- the runtime type (`ServiceStackTypeVersionName`)
- whether ports are exposed (dynamic/implicit-web vs static)
