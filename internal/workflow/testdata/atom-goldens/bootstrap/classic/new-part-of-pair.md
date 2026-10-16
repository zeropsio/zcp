---
id: bootstrap/classic/new-part-of-pair
atomIds: [bootstrap-intro, bootstrap-classic-plan-dynamic, bootstrap-classic-plan-static, bootstrap-new-part-of-pair, bootstrap-mode-prompt, bootstrap-runtime-classes]
description: "Classic route, discover step, in a project whose single-part standard pair is deployed — a new part of that repo is a stage of the pair."
---
=== bootstrap-intro ===
Bootstrap is **infrastructure-only**: create services, mount filesystems, discover env var keys, write the evidence file. No application code, no `zerops.yaml`, no first deploy — those belong to the develop workflow.

Three routes:

- **Recipe** — services come from a matched recipe's import YAML.
- **Classic** — agent constructs the import YAML from the user's intent.
- **Adopt** — attach `ServiceMeta` to existing non-managed services; no infra change.

Route is chosen at bootstrap start and persists for the session. The 3 steps are `discover → provision → close` in fixed order; follow the step list from `zerops_workflow action="status"`. (This overview fires only at the discover step — once route + plan are committed and you advance to `provision` / `close`, the step-specific atoms own the rendered guidance.)

---

=== bootstrap-classic-plan-dynamic ===
### Dynamic runtime plan

If the plan you're about to submit includes a dynamic runtime (Node, Go, Python, Bun, Ruby, …), apply this section. Classic bootstrap creates the runtime + managed services with `startWithoutCode: true` so dev containers reach RUNNING with an empty filesystem; `workflow=develop` then scaffolds `zerops.yaml`, writes the application, and runs the first deploy.

`bootstrapMode` and `stageHostname` MUST be inside `runtime` — flat placement is hard-rejected by the schema. Nest them under `runtime` (see the shape below) the first time.

```json
[
  {
    "runtime": {
      "devHostname": "appdev",
      "stageHostname": "appstage",
      "type": "ubuntu/nodejs@22",
      "bootstrapMode": "standard",
      "isExisting": false
    },
    "dependencies": [
      {"hostname": "db", "type": "postgresql:single@18", "resolution": "CREATE"}
    ]
  }
]
```

A repo with several deployable parts: read
`zerops_knowledge uri="zerops://playbooks/monorepo"` before you submit — the
plan then names one stage per part with its setup in `stages` instead of
`stageHostname`.

Confirm dev/stage pairing with the user before submitting the plan. Mode + close-mode + git-push capability decisions all happen later in develop, not here.

Add `"publicAccess": "none"` inside `runtime` when the user wants this runtime kept internal-only (no public subdomain ever) — omit the field entirely for the default (a public subdomain switches on automatically the first time it can).

---

=== bootstrap-classic-plan-static ===
### Static runtime plan

If the plan you're about to submit includes a static-runtime container (`nginx`, `static`), apply this section. Static-runtime containers come up serving an empty document root after bootstrap. The first build artifact lands in develop via `zerops_deploy`; bootstrap creates the empty container and stops there.

Before submitting the plan, confirm with the user:

- the chosen runtime hostname (`appdev` is the standard convention)
- whether a stage pair is wanted (`standard` mode) or a single container (`simple` / `dev` mode)

Close-mode, git-push capability, and the actual `zerops.yaml` (including `deployFiles` shape) are decided in develop after the first deploy lands — not here.

---

=== bootstrap-new-part-of-pair ===
### A new part of the same repo

If the new piece (a worker, an api) is built from the repo of the pair
below, it is a second part of that app, never a second bootstrap:

{services-list:- `appdev`}

1. Import the part's stage service (`zerops_import`, `startWithoutCode: true`)
   under its own hostname, such as `workerstage`.
2. Plan the pair as the one runtime it is: its dev hostname with
   `isExisting: true` and `stages` listing every stage with its setup, the
   new one included (`{"hostname": "workerstage", "setupName": "worker"}`).
   Never a second runtime for the part.
3. In develop, add the part's `setup:` to the repo's `zerops.yaml`, then in
   the same step give the part its own entry in `.zcp/dev-servers.json`
   (autostart, unless the dev service's RAM is short) and run
   `zerops_dev_server action=start hostname="<dev>" name=<part>`. A worker without a port is
   still a named server. Every part on the dev service runs as one.

Read `zerops_knowledge uri="zerops://playbooks/monorepo"` first. Bootstrap a
new runtime only for a different repo.

---

=== bootstrap-mode-prompt ===
### Confirm mode per service

Every runtime service needs a **mode**; confirm with the user before
submitting the plan.

- **dev** — single mutable dev container, SSHFS-mountable, no stage pair.
  The app runs ONLY as the dev server you start with `zerops_dev_server`
  (no supervised `run.start`); zcp starts it again after a container
  cycle, and a crash while starting is retried once, but a crash after it
  listened leaves it down and the URL answers 502 while it is.
  Pick dev for hands-on iteration with no durable end-state — never as the
  final state of a service the user wants to stay reachable.
- **standard** — dev + stage pair. The envelope reports `stageHostname`
  on the dev snapshot and a separate snapshot with `mode: stage` for
  the stage service.
  - **Plan MUST set `stageHostname` explicitly on every standard target**
    (e.g. `{"runtime": {"devHostname": "appdev", "type": "...", "bootstrapMode": "standard", "stageHostname": "appstage"}}`).
    A submission omitting `stageHostname` rejects with an actionable
    error pointing back to `bootstrapMode="dev"` if a single container
    was the actual intent.
- **simple** — single always-on container: `run.start` runs the real app
  on every deploy, platform-supervised so it survives container cycles.
  Pick simple for a durable single service the user wants reachable at a
  URL (web app, API, dashboard) and for background workers.
- **stage** — never bootstrapped alone; it is the stage half of a
  standard pair.

**standard is the recommended default**: a dev container to iterate in plus
a supervised stage that stays reachable after every container cycle. Pick
**simple** only when the user asks for exactly one service (a single
always-on app, a background worker); pick **dev** only for an explicit
scratch space with no durable end-state — dev's transience is a footgun
for anything left running. The plan commits the mode when you submit it;
the envelope then exposes it as `ServiceSnapshot.Mode`. Changing mode
later requires a mode-expansion bootstrap session, surfaced in develop
when actionable.

---

=== bootstrap-runtime-classes ===
### Runtime classes

Each runtime type falls into one of four classes — pick the right class for each runtime in the plan:

- **Dynamic** (nodejs, go, python, bun, ruby, …) — needs an explicit dev-server lifecycle in develop (container: `zerops_dev_server`; local: harness background task).
- **Static** (nginx, static) — serves files from `deployFiles`; platform auto-starts after deploy.
- **Implicit-webserver** (php-apache, php-nginx) — webserver is part of the runtime; platform auto-starts after deploy.
- **Managed** (postgresql, mariadb, redis/valkey, keydb, rabbitmq, nats, object storage) — no deploy; scale and connect only.

Pick runtime types from the live Zerops catalog (check `zerops_knowledge` for current versions). Managed services initialize first (`priority: 10` in import YAML) so runtimes that depend on them can connect at start.

Lifecycle and `zerops.yaml` mechanics for each class (start commands, healthCheck, deployFiles, dev-server primitives) are delivered by the develop response at first-deploy time.
