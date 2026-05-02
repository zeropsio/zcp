---
id: bootstrap/recipe/close
atomIds: [bootstrap-recipe-close, develop-api-error-meta, bootstrap-intro, bootstrap-verify, bootstrap-close]
description: "Recipe route, close step — bootstrap finishing, agent prompted for handoff to develop."
---
<!-- UNREVIEWED -->

### Close the recipe bootstrap

Complete the close step:

```
zerops_workflow action="complete" step="close" attestation="Recipe bootstrapped — services active and verified"
```

After close, every service the recipe provisioned appears in the envelope with `bootstrapped: true` and `closeMode: unset`. Close-mode and git-push capability are configured in develop after the first deploy lands — `develop-strategy-review` surfaces the menu when actionable. Start develop next:

```
zerops_workflow action="start" workflow="develop"
```

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

Each `apiMeta[].metadata` key is a **field path** (`appdev.mode`,
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

Bootstrap is **infrastructure-only** (Option A since v8.100+): create services, mount filesystems, discover env var keys, write the evidence file. No application code, no `zerops.yaml`, no first deploy — those belong to the develop workflow.

Three routes:

- **Recipe** — services come from a matched recipe's import YAML.
- **Classic** — agent constructs the import YAML from the user's intent.
- **Adopt** — attach `ServiceMeta` to existing non-managed services; no infra change.

Route is chosen at bootstrap start and persists for the session. The 3 steps are `discover → provision → close` in fixed order; follow the step list from `zerops_workflow action="status"`.

---

### Verify infrastructure before closing bootstrap

Bootstrap is infra-only: no code, no deploy, no HTTP probe. Close must
confirm the **platform layer** is healthy before develop starts.

```
zerops_discover
```

Required state for every planned service:

- Platform `status` = `RUNNING` for managed services (databases, caches,
  object storage). A managed service that never reached `RUNNING` means
  the import failed silently — investigate `zerops_process` logs, do
  not close.
- Runtime services may appear as `NOT_YET_DEPLOYED` — that is expected.
  Code and the first deploy happen in the develop workflow.
- Env vars discovered during provisioning must be recorded in the
  session so develop can wire them without re-discovering.

Do **not** run `zerops_verify` here — that tool probes the app layer
(HTTP reachability, `/status` endpoints) which only makes sense **after**
develop writes code and runs the first deploy. Running it during
bootstrap will report every runtime as failing and is noise.

If a managed service is stuck in a non-`RUNNING` state, bootstrap
hard-stops: surface the failure to the user rather than retrying —
infrastructure issues require the user's judgment.

---

### Closing bootstrap

Bootstrap is **infrastructure-only**. After
`action="complete" step="close"`, planned runtimes show
`bootstrapped: true`: managed services are `RUNNING`, runtimes are
registered, dev containers are SSH-mount-ready, and managed env vars
are discoverable. Classic and recipe-with-first-deploy-later services
show `deployed: false` and enter develop's first-deploy branch. Adopted
services and recipes that deployed during bootstrap show `deployed: true`.

No application code is written, no `zerops.yaml` generated, and no
deploy runs as part of bootstrap close itself.

**Next step — `zerops_workflow action="start" workflow="develop"`.** Develop owns code, the first deploy, verify, iteration, and close-mode setup; `develop-first-deploy-intro` fires on entry for services with `deployed: false`.

Direct tools (`zerops_scale`, `zerops_env`, `zerops_subdomain`, `zerops_discover`) stay callable without a workflow wrapper for one-shot infra changes.

Complete this step before starting develop.
