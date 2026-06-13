---
id: bootstrap/classic/discover-standard-dynamic
atomIds: [bootstrap-intro, bootstrap-classic-plan-dynamic, bootstrap-classic-plan-static, bootstrap-mode-prompt, bootstrap-runtime-classes]
description: "Classic route, discover step — agent inspecting an empty project for a dynamic runtime in mode=standard."
---
Bootstrap is **infrastructure-only**: create services, mount filesystems, discover env var keys, write the evidence file. No application code, no `zerops.yaml`, no first deploy — those belong to the develop workflow.

Three routes:

- **Recipe** — services come from a matched recipe's import YAML.
- **Classic** — agent constructs the import YAML from the user's intent.
- **Adopt** — attach `ServiceMeta` to existing non-managed services; no infra change.

Route is chosen at bootstrap start and persists for the session. The 3 steps are `discover → provision → close` in fixed order; follow the step list from `zerops_workflow action="status"`. (This overview fires only at the discover step — once route + plan are committed and you advance to `provision` / `close`, the step-specific atoms own the rendered guidance.)

---

### Dynamic runtime plan

If the plan you're about to submit includes a dynamic runtime (Node, Go, Python, Bun, Ruby, …), apply this section. Classic bootstrap creates the runtime + managed services with `startWithoutCode: true` so dev containers reach RUNNING with an empty filesystem; `workflow=develop` then scaffolds `zerops.yaml`, writes the application, and runs the first deploy.

`bootstrapMode` and `stageHostname` MUST be inside `runtime` — flat placement is hard-rejected by the schema. Nest them under `runtime` (see the shape below) the first time.

```json
[
  {
    "runtime": {
      "devHostname": "appdev",
      "stageHostname": "appstage",
      "type": "nodejs@22",
      "bootstrapMode": "standard",
      "isExisting": false
    },
    "dependencies": [
      {"hostname": "db", "type": "postgresql:single@18", "resolution": "CREATE"}
    ]
  }
]
```

Confirm dev/stage pairing with the user before submitting the plan. Mode + close-mode + git-push capability decisions all happen later in develop, not here.

---

### Static runtime plan

If the plan you're about to submit includes a static-runtime container (`nginx`, `static`), apply this section. Static-runtime containers come up serving an empty document root after bootstrap. The first build artifact lands in develop via `zerops_deploy`; bootstrap creates the empty container and stops there.

Before submitting the plan, confirm with the user:

- the chosen runtime hostname (`appdev` is the standard convention)
- whether a stage pair is wanted (`standard` mode) or a single container (`simple` / `dev` mode)

Close-mode, git-push capability, and the actual `zerops.yaml` (including `deployFiles` shape) are decided in develop after the first deploy lands — not here.

---

### Confirm mode per service

Every runtime service needs a **mode**; confirm with the user before
submitting the plan.

- **dev** — single mutable dev container, SSHFS-mountable, no stage pair.
  The app runs ONLY via `zerops_dev_server` (no supervised `run.start`),
  so the public URL **502s after any container cycle** until restarted.
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

Choose on the OUTCOME, not iteration habit: a service that should stay
reachable → **simple** (or **standard** for a dev+stage split); a scratch
space for hands-on iteration with no durable end-state → **dev**. For a
"build me X" request that ends at a URL, **simple** is the safe default —
dev's transience is a footgun for anything left running. The plan commits
the mode when you submit it; the envelope then exposes it as
`ServiceSnapshot.Mode`. Changing mode later requires a mode-expansion
bootstrap session, surfaced in develop when actionable.

---

### Runtime classes

Each runtime type falls into one of four classes — pick the right class for each runtime in the plan:

- **Dynamic** (nodejs, go, python, bun, ruby, …) — needs an explicit dev-server lifecycle in develop (container: `zerops_dev_server`; local: harness background task).
- **Static** (nginx, static) — serves files from `deployFiles`; platform auto-starts after deploy.
- **Implicit-webserver** (php-apache, php-nginx) — webserver is part of the runtime; platform auto-starts after deploy.
- **Managed** (postgresql, mariadb, redis/valkey, keydb, rabbitmq, nats, object storage) — no deploy; scale and connect only.

Pick runtime types from the live Zerops catalog (check `zerops_knowledge` for current versions). Managed services initialize first (`priority: 10` in import YAML) so runtimes that depend on them can connect at start.

Lifecycle and `zerops.yaml` mechanics for each class (start commands, healthCheck, deployFiles, dev-server primitives) are delivered by the develop response at first-deploy time.
