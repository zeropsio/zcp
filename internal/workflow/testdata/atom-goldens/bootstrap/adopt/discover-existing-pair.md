---
id: bootstrap/adopt/discover-existing-pair
atomIds: [bootstrap-intro, bootstrap-adopt-discover]
description: "Adopt route, discover step — pre-existing dev/stage pair present in the project, agent adopting."
---
Bootstrap is **infrastructure-only**: create services, mount filesystems, discover env var keys, write the evidence file. No application code, no `zerops.yaml`, no first deploy — those belong to the develop workflow.

Three routes:

- **Recipe** — services come from a matched recipe's import YAML.
- **Classic** — agent constructs the import YAML from the user's intent.
- **Adopt** — attach `ServiceMeta` to existing non-managed services; no infra change.

Route is chosen at bootstrap start and persists for the session. The 3 steps are `discover → provision → close` in fixed order; follow the step list from `zerops_workflow action="status"`. (This overview fires only at the discover step — once route + plan are committed and you advance to `provision` / `close`, the step-specific atoms own the rendered guidance.)

---

### Adopting existing services

Adoption attaches ZCP tracking to an existing runtime service without touching its code, configuration, or scale. After adopt close, the envelope reports each adopted hostname with `bootstrapped: true` and an empty close-mode / git-push capability — populated later when the develop session needs them.

List what's there:

```
zerops_discover
```

Read every user (non-system, non-managed) service. For each, note:

- the hostname (keep verbatim; do not rename)
- the runtime type (`ServiceStackTypeVersionName`)
- whether ports are exposed (dynamic/implicit-web vs static)
