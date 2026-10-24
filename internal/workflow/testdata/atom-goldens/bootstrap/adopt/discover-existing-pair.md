---
id: bootstrap/adopt/discover-existing-pair
atomIds: [bootstrap-intro, bootstrap-adopt-discover, bootstrap-adopt-new-part]
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

Adoption deploys nothing. A service discover reports `deployed: false` was imported without code: it is ACTIVE, and its working directory may already hold source, but nothing was ever built or started in it. It stays `deployed: false` after adopt, and the develop workflow opens with its first deploy.

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

**Branch on what discover shows.** If two or more of the runtimes you adopt share a stack type (the dev/stage shape — a dev service and its stage, or several of them), skip `scope` and submit an explicit `plan=[...]` — a bare scope can't tell a standard dev/stage pair from independent dev containers, so it rejects. (If you do call with `scope` here, the reject hands back two ready-to-paste templates covering every named service — dev/stage pairs vs independent devs — to pick from; submitting the plan directly skips that round-trip.) Adopt-route plan shape — every runtime entry needs `isExisting: true` (adoption tracks a live service, it does not create one), and managed deps use `resolution: "EXISTS"`:

```
plan=[{"runtime": {"devHostname": "appdev", "stageHostname": "appstage", "type": "ubuntu/nodejs@22", "bootstrapMode": "standard", "isExisting": true},
       "dependencies": [{"hostname": "db", "type": "postgresql:single@18", "resolution": "EXISTS"}]}]
```

For independent devs, emit one entry each with its own `runtime` and no `stageHostname`. In every other case (one runtime, or runtimes of different stacks), `scope` is enough: you do not hand-write the plan — `scope` is just the hostname list and the plan is derived for you.

One exception keeps `scope` whatever the stacks: dev services whose `zerops.yaml` mounts a local-storage service that already holds their repository (a shared code volume). No plan records that pair — name its dev services and their stages in `scope`, and the pair is derived with its stages; adopt any other runtimes in their own call.

Each named service becomes a tracked runtime target; `adoptionState="managed-dep"` services attach as shared dependencies. Naming the services keeps adoption scoped to YOUR task: in a project with other live work (or another agent session), an empty scope is ambiguous, so it returns the adoptable candidate list for you to pick from rather than silently adopting everything. The control-plane (`zcp-self` / `zcp@*`), already-`adopted`, and `resumable` (mid-bootstrap, owned by a prior session → use `resume`) services are never adopt targets. Hostnames stay verbatim — never rename an adopted service.

---

=== bootstrap-adopt-new-part ===
### A new part next to the adopted pair

If the task also asks for another piece (a worker, an api) built from the
repo of the pair you adopt, it is a second part of that app, never a second
runtime of the plan:

1. Import the part's stage service (`zerops_import`) under its own
   hostname, such as `workerstage`, without `startWithoutCode`: like every
   stage it waits at READY_TO_DEPLOY for its first deploy.
2. Plan the pair as the one runtime it is: its dev hostname with
   `isExisting: true` and `stages` listing every stage with its setup, the
   new one included (`{"hostname": "workerstage", "setupName": "worker"}`).
   Never a second runtime for the part.
   An existing setup and its stage keep their names; only the new part gets
   a new setup. A stage that deploys `setup: prod` stays
   `{"hostname": "appstage", "setupName": "prod"}` — never rename a `setup:`
   the repo already has.
3. In develop, add the part's `setup:` to the repo's `zerops.yaml`, then in
   the same step give the part its own entry in `.zcp/dev-servers.json`
   (autostart, unless the dev service's RAM is short) and run
   `zerops_dev_server action=start hostname="<dev>" name=<part>`. A worker without a port is
   still a named server. Every part on the dev service runs as one.

Read `zerops_knowledge uri="zerops://playbooks/monorepo"` first. Plan a new
runtime only for a different repo.
