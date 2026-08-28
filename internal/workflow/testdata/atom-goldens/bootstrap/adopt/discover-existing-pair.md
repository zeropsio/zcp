---
id: bootstrap/adopt/discover-existing-pair
atomIds: [bootstrap-intro, bootstrap-adopt-discover]
description: "Adopt route, discover step — pre-existing dev/stage pair present in the project, agent adopting."
---
=== bootstrap-intro ===
Bootstrap is **infrastructure-only**: create services, mount filesystems, discover env var keys, write the evidence file. No application code, no `zerops.yaml`, no first deploy — those belong to the develop workflow.

Three routes:

- **Recipe** — services come from a matched recipe's import YAML.
- **Classic** — agent constructs the import YAML from the user's intent.
- **Adopt** — attach `ServiceMeta` to existing non-managed services; no infra change.

Route is chosen at bootstrap start and persists for the session. The 3 steps are `discover → provision → close` in fixed order; follow the step list from `zerops_workflow action="status"`. (This overview fires only at the discover step — once route + plan are committed and you advance to `provision` / `close`, the step-specific atoms own the rendered guidance.)

---

=== bootstrap-adopt-discover ===
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

Then complete the discover step naming the services you want to adopt in `scope` (the
`adoptionState="adoptable"` hostnames from discover that THIS task needs):

```
zerops_workflow action="complete" step="discover" scope=["appdev","appstage"]
```

**Branch on what discover shows.** If discover flags exactly two adoptable runtimes that share a stack type (the dev/stage shape), skip `scope` and submit an explicit `plan=[...]` — a bare scope can't tell a standard dev/stage pair from two independent dev containers, so it rejects. (If you do call with `scope` here, the reject hands back two ready-to-paste templates — standard pair vs independent devs — to pick from; submitting the plan directly skips that round-trip.) Adopt-route plan shape — every runtime entry needs `isExisting: true` (adoption tracks a live service, it does not create one), and managed deps use `resolution: "EXISTS"`:

```
plan=[{"runtime": {"devHostname": "appdev", "stageHostname": "appstage", "type": "ubuntu/nodejs@22", "bootstrapMode": "standard", "isExisting": true},
       "dependencies": [{"hostname": "db", "type": "postgresql:single@18", "resolution": "EXISTS"}]}]
```

For two independent devs, emit two entries each with its own `runtime` and no `stageHostname`. In every other case (one runtime, or runtimes of different stacks), `scope` is enough: you do not hand-write the plan — `scope` is just the hostname list and the plan is derived for you.

Each named service becomes a tracked runtime target; `adoptionState="managed-dep"` services attach as shared dependencies. Naming the services keeps adoption scoped to YOUR task: in a project with other live work (or another agent session), an empty scope is ambiguous, so it returns the adoptable candidate list for you to pick from rather than silently adopting everything. The control-plane (`zcp-self` / `zcp@*`), already-`adopted`, and `resumable` (mid-bootstrap, owned by a prior session → use `resume`) services are never adopt targets. Hostnames stay verbatim — never rename an adopted service.
