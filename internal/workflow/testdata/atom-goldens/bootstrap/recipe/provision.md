---
id: bootstrap/recipe/provision
atomIds: [bootstrap-recipe-import, develop-api-error-meta, bootstrap-intro]
description: "Recipe route, provision step in progress, target service ACTIVE awaiting first deploy."
---
<!-- UNREVIEWED -->

### Provision recipe services

Procedure is fixed; do NOT rewrite or reorder.

1. **Project-level env vars (if any).**

If the YAML begins with a `project:` block containing `envVariables:`, set
them at project scope BEFORE `zerops_import`; the import tool rejects
project-level blocks.

```
zerops_env action="set" scope="project" key="APP_KEY" value="<@generateRandomString(<32>)>"
```

Preprocessor directives (`<@...>`) evaluate server-side; pass the literal
string, not a pre-rendered value. Repeat for each project env var.

2. **Import services.**

Strip `project:`. Submit `services:` verbatim via `zerops_import` — ZCP
already applied plan hostnames and dropped EXISTS-resolved managed
services. Don't edit resource limits, `buildFromGit`, `priority`,
`zeropsSetup`, or `type`.

3. **Wait until every service reports `ACTIVE`.** Poll:

```
zerops_discover
```

Every runtime must reach `status: ACTIVE` before `deploy`; managed deps
usually transition first.

4. **Record discovered env vars.**

After ACTIVE, include managed-service env var keys in the provision
attestation (e.g. `db: connectionString, port`) for later
`run.envVariables` references.

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
