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

If you reached this atom by way of the `ADOPT_REQUIRED` rejection on a service-scoped tool, the right reflex was to fire adopt directly from the discover warning. The bootstrap-adopt session opens with a single committed call:

```
zerops_workflow action="start" workflow="bootstrap" route="adopt" intent="<one-line user task summary>"
```

Use the SAME `intent` string you'd pass to `workflow="develop"` afterwards — the intent threads through so you don't re-type it on the next workflow call. Placeholder strings (`"<task>"`, `"adopt existing"`) lose user context and break the develop-session continuity heuristic; phrase it as the actual scope the user requested.

List what's there:

```
zerops_discover
```

Then complete the discover step with **no `plan`**:

```
zerops_workflow action="complete" step="discover"
```

You do not hand-write the adopt plan. Every service marked `adoptionState="adoptable"` becomes a tracked runtime target; `adoptionState="managed-dep"` services attach as shared dependencies. The remaining states are excluded for you: `"adopted"` (already tracked), the control-plane `"zcp-self"`, and `"resumable"` (mid-bootstrap, owned by a prior session — that routes through `resume`, not `adopt`). Hostnames stay verbatim — never rename an adopted service.

When exactly two adoptable runtimes share one runtime type (`ServiceStackTypeVersionName`) — the dev/stage shape — the response hands back two ready-to-paste plan templates rather than one default: a `standard` dev/stage pair (one container builds, the other receives the cross-deploy promote) and two independent dev containers. Pick the shape matching the user's intent and resubmit it as `plan=[...]`.
