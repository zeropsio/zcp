---
id: bootstrap/classic/discover-standard-dynamic
atomIds: [bootstrap-intro, develop-api-error-meta, bootstrap-mode-prompt, bootstrap-runtime-classes]
description: "Classic route, discover step — agent inspecting an empty project for a dynamic runtime in mode=standard."
---
<!-- UNREVIEWED -->

Bootstrap is **infrastructure-only** (Option A since v8.100+): create services, mount filesystems, discover env var keys, write the evidence file. No application code, no `zerops.yaml`, no first deploy — those belong to the develop workflow.

Three routes:

- **Recipe** — services come from a matched recipe's import YAML.
- **Classic** — agent constructs the import YAML from the user's intent.
- **Adopt** — attach `ServiceMeta` to existing non-managed services; no infra change.

Route is chosen at bootstrap start and persists for the session. The 3 steps are `discover → provision → close` in fixed order; follow the step list from `zerops_workflow action="status"`.

---

### Read `apiMeta` on every error response

Any `zerops_*` tool surfacing a Zerops API 4xx may include `apiMeta`.
Missing key = no server detail; present key = exact rejected fields.

Shape:

```json
{
  "code": "API_ERROR",
  "apiCode": "projectImportInvalidParameter",
  "error": "Invalid parameter provided.",
  "suggestion": "Zerops flagged specific fields — see apiMeta for each field's failure reason.",
  "apiMeta": [
    {
      "code": "projectImportInvalidParameter",
      "error": "Invalid parameter provided.",
      "metadata": {
        "storage.mode": ["mode not supported"]
      }
    }
  ]
}
```

Each `apiMeta[].metadata` key is a **field path** (`.mode`,
`build.base`, `parameter`); values list reasons. Fix those YAML fields
and retry — do not guess.

Common `apiCode` shapes:

| `apiCode` | `metadata` key | Meaning |
|---|---|---|
| `projectImportInvalidParameter` | `<host>.mode` | type/mode combination not allowed |
| `projectImportMissingParameter` | `parameter` (value `<host>.mode`) | required field missing |
| `serviceStackTypeNotFound` | `serviceStackTypeVersion` | version string not in platform catalog |
| `zeropsYamlInvalidParameter` | `build.base` etc. | zerops.yaml validator caught the field pre-build |
| `yamlValidationInvalidYaml` | `reason` (with `line N:`) | YAML syntax error |

Per-service import failures use `serviceErrors[].meta` with the same
shape, one entry per failing service-stack.

---

### Confirm mode per service

Every runtime service needs a **mode**; confirm with the user before
submitting the plan.

- **dev** — single mutable dev container, SSHFS-mountable, no stage pair.
  Best for active iteration.
- **standard** — dev + stage pair. The envelope reports `stageHostname`
  on the dev snapshot and a separate snapshot with `mode: stage` for
  the stage service.
  - **Plan MUST set `stageHostname` explicitly on every standard target**
    (e.g. `{"runtime": {"devHostname": "appdev", "type": "...", "bootstrapMode": "standard", "stageHostname": "appstage"}}`).
    Hostname-suffix derivation (`appdev` → `appstage`) was removed in
    Release B.4. A submission omitting `stageHostname` rejects with
    `INVALID_PARAMETER: standard mode requires explicit stageHostname`.
- **simple** — single runtime container that starts real code on every redeploy;
  no SSHFS mutation lifecycle.
- **stage** — never bootstrapped alone; it is the stage half of a
  standard pair.

Default to **dev** for services under active iteration, **simple** for
immutable workers. The plan commits the mode when you submit it; after
bootstrap closes, the envelope exposes the chosen mode as
`ServiceSnapshot.Mode`. Changing mode later requires the
mode-expansion flow (see `develop-mode-expansion`).

---

### Runtime classes

Each runtime type falls into one of four classes — pick the right class for each runtime in the plan:

- **Dynamic** (nodejs, go, python, bun, ruby, …) — needs an explicit dev-server lifecycle in develop (container: `zerops_dev_server`; local: harness background task).
- **Static** (nginx, static) — serves files from `deployFiles`; platform auto-starts after deploy.
- **Implicit-webserver** (php-apache, php-nginx) — webserver is part of the runtime; platform auto-starts after deploy.
- **Managed** (postgresql, mariadb, redis/valkey, keydb, rabbitmq, nats, object storage) — no deploy; scale and connect only.

Pick runtime types from the live Zerops catalog (check `zerops_knowledge` for current versions). Managed services initialize first (`priority: 10` in import YAML) so runtimes that depend on them can connect at start.

Lifecycle and `zerops.yaml` mechanics for each class (start commands, healthCheck, deployFiles, dev-server primitives) are delivered in develop atoms — `develop-dynamic-runtime-start-container`, `develop-dynamic-runtime-start-local`, `develop-implicit-webserver`, `develop-first-deploy-scaffold-yaml` — at first-deploy time.
